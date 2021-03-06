package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

///////////////////////////////////////////////////////////////////////////////
// Handlers
///////////////////////////////////////////////////////////////////////////////

func registerHandler(c *fiber.Ctx) error {
	// Check if the netID is a student's
	// Create a 4 digit pin to validate with
	// Send an email to the netID with the pin
	data := new(map[string]interface{})
	if err := c.BodyParser(data); err != nil {
		log.WithError(err).Error("Error parsing body")
		return c.SendStatus(fiber.StatusBadRequest)
	}

	username := fmt.Sprint((*data)["username"])
	password := fmt.Sprint((*data)["password"])
	if username == "<nil>" || password == "<nil>" {
		err := errors.New("Posted nil `username` or `password`")
		log.WithError(err).Error("Registration error")
		return c.SendStatus(fiber.StatusBadRequest)
	}

	if val, err := dbHasUsername(username); err != nil {
		log.WithError(err).Error("Error checking if db has username")
		return c.SendStatus(fiber.StatusInternalServerError)
	} else if val {
		log.Error("username already registered")
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	if err := checkNetID(username); err != nil {
		log.WithError(err).Error("Error validating netID")
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	pin := fmt.Sprintf("%08d", randInt(0, 99999999))
	if err := addRegistrationPin(username, pin); err != nil {
		log.WithError(err).Error("Error adding registration pin to redis")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	hashedPassword, err := hashPassword(password)
	if err != nil {
		log.WithError(err).Error("Error hashing password")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	if err := cachePassword(username, hashedPassword); err != nil {
		log.WithError(err).Error("Error caching login credentials")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// Format variables to send an email
	to := []string{
		fmt.Sprintf("%v@duke.edu", username),
	}
	link := fmt.Sprintf("%v/confirm/%v/%v", origin, username, pin)
	body := fmt.Sprintf("To confirm your registration, click this link: <a href=\"%v\">%v</a>", link, link)

	if err := sendEmail(to, "Register for GroupDuke", body); err != nil {
		log.WithError(err).Error("Error sending registration email")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}

func confirmRegistrationHandler(c *fiber.Ctx) error {
	data := new(map[string]interface{})
	if err := c.BodyParser(data); err != nil {
		log.WithError(err).Error("Error parsing body")
		return c.SendStatus(fiber.StatusBadRequest)
	}

	username := fmt.Sprint((*data)["username"])
	pin := fmt.Sprint((*data)["pin"])
	if username == "<nil>" || pin == "<nil>" {
		err := errors.New("`username` or `pin` not sent with post")
		log.WithError(err).Error("Error parsing data")
		return c.SendStatus(fiber.StatusBadRequest)
	}

	if val, err := getRegistrationPin(username); err != nil {
		log.WithError(err).Error("Error checking redis for pin")
		return c.SendStatus(fiber.StatusInternalServerError)
	} else if val != pin {
		err = errors.New("Pin in redis != posted value")
		log.WithError(err).Error("Confirm registration failed")
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	password, err := getCachedPassword(username)
	if err != nil {
		log.WithError(err).Error("Error getting cached password from redis")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	if err := setLogin(username, password); err != nil {
		log.WithError(err).Error("Error adding login to database")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	if err := removeRegistrationPin(username); err != nil {
		msg := fmt.Sprintf("Error removing registration pin for %v", username)
		log.WithError(err).Error(msg)
	}
	log.Info(fmt.Sprintf("Login info added for %v", username))

	if err := removeCachedPassword(username); err != nil {
		msg := fmt.Sprintf("Error removing cached password for %v", password)
		log.WithError(err).Error(msg)
	}

	return c.SendStatus(fiber.StatusOK)
}

func resetPasswordHandler(c *fiber.Ctx) error {
	// need to generate a pin to associate with the reset
	// store the __reset_pin__NETID in redis with pin
	// need to send email with link to reset password
	data := new(map[string]interface{})
	if err := c.BodyParser(data); err != nil {
		log.WithError(err).Error("Error parsing body")
		return c.SendStatus(fiber.StatusBadRequest)
	}

	username := fmt.Sprint((*data)["username"])
	if username == "<nil>" {
		log.Error("Error parsing username")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	if val, err := dbHasUsername(username); err != nil {
		log.WithError(err).Error("Error checking if username is in db")
		return c.SendStatus(fiber.StatusInternalServerError)
	} else if !val {
		log.Error("Can't reset password if user not in database")
		return c.SendStatus(fiber.StatusBadRequest)
	}

	pin := fmt.Sprintf("%08d", randInt(0, 99999999))
	if err := addResetPasswordPin(username, pin); err != nil {
		log.WithError(err).Error("Error adding reset password pin to redis")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	to := []string{
		fmt.Sprintf("%v@duke.edu", username),
	}
	link := fmt.Sprintf("%v/reset-password/%v/%v", origin, username, pin)
	body := fmt.Sprintf("To reset your password, click this link: <a href=\"%v\">%v</a>", link, link)

	if err := sendEmail(to, "Reset GroupDuke password", body); err != nil {
		log.WithError(err).Error("Error sending reset password email")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}

func confirmResetPasswordHandler(c *fiber.Ctx) error {
	data := new(map[string]interface{})
	if err := c.BodyParser(data); err != nil {
		log.WithError(err).Error("Error parsing body")
		return c.SendStatus(fiber.StatusBadRequest)
	}

	username := fmt.Sprint((*data)["username"])
	password := fmt.Sprint((*data)["password"])
	pin := fmt.Sprint((*data)["pin"])
	if username == "<nil>" || password == "<nil>" || pin == "<nil>" {
		err := errors.New("`username`, `password`, or `pin` not sent with post")
		log.WithError(err).Error("Error parsing data")
		return c.SendStatus(fiber.StatusBadRequest)
	}

	if val, err := getResetPasswordPin(username); err != nil {
		log.WithError(err).Error("Error checking redis for pin")
		return c.SendStatus(fiber.StatusInternalServerError)
	} else if val != pin {
		err = errors.New("Pin in redis != posted value")
		log.WithError(err).Error("Confirm reset password failed")
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	hashedPassword, err := hashPassword(password)
	if err != nil {
		log.WithError(err).Error("Erroring hashing password")
	}

	if err := setLogin(username, hashedPassword); err != nil {
		log.WithError(err).Error("Erroring changing login")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	if err := removeResetPasswordPin(username); err != nil {
		log.WithError(err).Error("Error removing reset password pin from redis")
	}

	return c.SendStatus(fiber.StatusOK)
}

func loginHandler(c *fiber.Ctx) error {
	type Credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	creds := new(Credentials)
	if err := c.BodyParser(creds); err != nil {
		log.Error(fmt.Sprintf("Error parsing login: %+v", err))
		return c.SendStatus(fiber.StatusBadRequest)
	}

	expectedPassword, err := getPassword(creds.Username)
	if err != nil {
		log.WithError(err).Error("Error fetching password from database")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(expectedPassword), []byte(creds.Password)); err != nil {
		log.Error(fmt.Sprintf("Passwords doesn't match for %v", creds.Username))
		return c.SendStatus(fiber.StatusUnauthorized)
	}

	expireTime := 60 * 60 // 1 hour
	sessionToken, err := addSessionToken(creds.Username, expireTime)
	if err != nil {
		log.WithError(err).Error("Failed to add new session_token to redis")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	c.Cookie(&fiber.Cookie{
		Name:    "session_token",
		Value:   sessionToken,
		Expires: time.Now().Add(time.Hour),
	})

	c.Cookie(&fiber.Cookie{
		Name:    "net_id",
		Value:   creds.Username,
		Expires: time.Now().Add(time.Hour),
	})

	log.Info(fmt.Sprintf("User %v logged in", creds.Username))
	return c.SendStatus(fiber.StatusOK)
}

func logoutHandler(c *fiber.Ctx) error {
	sessionToken := c.Cookies("session_token")
	if sessionToken == "" {
		return c.SendStatus(fiber.StatusOK)
	}

	if _, err := cache.Do("DEL", sessionToken); err != nil {
		log.WithError(err).Error("Failed to delete token in Redis")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}

func addCourseHandler(c *fiber.Ctx) error {
	course := new(Course)
	if err := c.BodyParser(course); err != nil {
		log.WithError(err).Error("Failed to parse course")
		return c.SendStatus(fiber.StatusBadRequest)
	}

	if err := addCourse(*course); err != nil {
		log.WithError(err).Error("Failed to add course")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	log.Info(fmt.Sprintf("Added %v", course.CourseNumber))
	return c.SendStatus(fiber.StatusOK)
}

func deleteCourseHandler(c *fiber.Ctx) error {
	return c.SendStatus(fiber.StatusOK)
}

func getDataHandler(c *fiber.Ctx) error {
	term := c.Params("term")

	courses, err := getCourses(term)
	if err != nil {
		log.WithError(err).Error("Failed to get courses from database")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.JSON(courses)
}

func contactHandler(c *fiber.Ctx) error {
	data := new(map[string]interface{})
	if err := c.BodyParser(data); err != nil {
		log.WithError(err).Error("Failed to parse incoming request body")
		return c.SendStatus(fiber.StatusBadRequest)
	}

	from := fmt.Sprint((*data)["email"])
	subject := fmt.Sprintf("USER MESSAGE: %v", (*data)["subject"])
	message := fmt.Sprint((*data)["message"])

	if from == "<nil>" || subject == "<nil>" || message == "<nil>" {
		return c.SendStatus(fiber.StatusBadRequest)
	}

	body := fmt.Sprintf("%v\n%v", from, message)

	myEmail := os.Getenv("EMAIL_USERNAME")
	to := []string{
		myEmail,
	}

	if err := sendEmail(to, subject, body); err != nil {
		log.WithError(err).Error("Error sending contact email")
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}

///////////////////////////////////////////////////////////////////////////////
// Middleware
///////////////////////////////////////////////////////////////////////////////

func authorize(fn func(c *fiber.Ctx) error) func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		sessionToken := c.Cookies("session_token")
		if sessionToken == "" {
			log.Error("'session_token' cookie not found")
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		res, err := cache.Do("GET", sessionToken)
		if err != nil {
			log.WithError(err).Error("Error checking redis")
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		if res == nil {
			log.WithError(err).Error("'session_token' cookie not found in redis")
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		return fn(c)
	}
}

func logRequests(c *fiber.Ctx) error {
	if c.Method() != "OPTION" {
		user := c.Cookies("net_id")
		if user != "" {
			log.WithField("user", user).Info(
				c.Method(), " ", c.OriginalURL())
		} else {
			log.Info(c.Method(), " ", c.OriginalURL())
		}
	}

	return c.Next()
}
