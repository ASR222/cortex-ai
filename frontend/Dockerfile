# Builds the React app and serves it with Caddy, which also terminates HTTPS
# (automatic Let's Encrypt certificates) and proxies /api to the gateway.

FROM node:24-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
# Vite inlines VITE_* variables at build time. These are public identifiers,
# not secrets (see firebase.js).
ARG VITE_FIREBASE_API_KEY
ARG VITE_FIREBASE_AUTH_DOMAIN
ARG VITE_FIREBASE_PROJECT_ID
ARG VITE_FIREBASE_APP_ID
RUN npm run build

FROM caddy:2-alpine
COPY --from=build /app/dist /srv
