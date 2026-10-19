FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/reports-api ./cmd/reports-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/reports-api /reports-api
EXPOSE 8000
USER nonroot:nonroot
ENTRYPOINT ["/reports-api"]
