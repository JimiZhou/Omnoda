FROM node:22-alpine AS web
WORKDIR /src/apps/web
COPY apps/web/package.json apps/web/package-lock.json* ./
RUN npm install
COPY apps/web ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/apps/web/dist ./apps/web/dist
RUN CGO_ENABLED=0 go build -o /out/omnoda-server ./cmd/omnoda-server && \
    CGO_ENABLED=0 go build -o /out/omnoda ./cmd/omnoda

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/omnoda-server /usr/local/bin/omnoda-server
COPY --from=build /out/omnoda /usr/local/bin/omnoda
COPY --from=build /src/apps/web/dist /srv/web
COPY scripts/install.sh scripts/install.ps1 /srv/
WORKDIR /srv
EXPOSE 8787
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/omnoda-server", "-data", "/data", "-addr", ":8787"]
