FROM node:16-alpine

WORKDIR /usr/src/app

COPY package*.json ./

RUN npm install

COPY . .
RUN npm run build

EXPOSE 3000
ENV HOST=0.0.0.0

CMD ["npm", "run", "preview", "--network", "0.0.0.0"]
