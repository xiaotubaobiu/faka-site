# 1) 构建 CSS(node)
FROM node:20-alpine AS css
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY tailwind.config.js ./
COPY src/input.css ./src/input.css
COPY internal/web internal/web
RUN npx tailwindcss -i src/input.css -o internal/web/static/app.css --minify

# 2) 构建 Go(含生成的 app.css)
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=css /src/internal/web/static/app.css ./internal/web/static/app.css
RUN CGO_ENABLED=0 go build -o /out/faka-site .

# 3) 运行(无 node,二进制内嵌静态)
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/faka-site /app/faka-site
EXPOSE 8080
VOLUME ["/app/data"]
ENV FAKA_DB=/app/data/data.db FAKA_LISTEN=:8080 COOKIE_SECURE=true
ENTRYPOINT ["/app/faka-site"]
