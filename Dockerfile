# Imagen de la API en dos etapas: compilar con Go y ejecutar en una imagen mínima.

# Etapa 1: compilación
FROM golang:1.27.3-alpine AS build
WORKDIR /src
# Primero solo las dependencias, para reutilizar la caché si no cambian.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Binario estático (sin cgo), sin rutas locales ni símbolos de depuración.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/accounts-api .

# Etapa 2: ejecución
FROM alpine:3.24
# Usuario sin privilegios de root.
RUN addgroup -S app && adduser -S -G app app
COPY --from=build /out/accounts-api /usr/local/bin/accounts-api
USER app
EXPOSE 3000
ENTRYPOINT ["accounts-api"]
