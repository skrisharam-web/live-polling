# Build the bundle, then serve it with nginx.
#
# The API origin is baked in at build time — Vite substitutes import.meta.env at
# compile time, so there is no runtime configuration to get wrong and no way to
# point a built image at a different backend. Build one image per environment.
#
# This is also why no secret may ever be passed here: every ARG below ends up in
# JavaScript that anyone can read.

FROM node:22-alpine AS build

WORKDIR /app

# npm ci rather than npm install: it installs exactly what the lockfile says and
# fails if the lockfile and package.json disagree, which is what you want in a
# build you are about to ship.
COPY package.json package-lock.json ./
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
RUN npm ci

COPY . .

ARG VITE_API_URL
ARG VITE_WS_URL
ENV VITE_API_URL=$VITE_API_URL
ENV VITE_WS_URL=$VITE_WS_URL

RUN npm run build

FROM nginx:1.27-alpine

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY security-headers.conf /etc/nginx/security-headers.conf
COPY --from=build /app/dist /usr/share/nginx/html

EXPOSE 80
