FROM node:22-alpine AS build
WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
ARG VITE_BASE_PATH=/cc-web/
ENV VITE_BASE_PATH=${VITE_BASE_PATH}
RUN npm exec tsc -- -b && npm exec vite -- build --base="${VITE_BASE_PATH}"

FROM nginx:alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
