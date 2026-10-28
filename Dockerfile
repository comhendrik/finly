FROM golang:1.23-alpine AS build
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY backend ./backend
COPY frontend ./frontend
RUN CGO_ENABLED=0 GOOS=linux go build -mod=mod -o /finly ./backend/cmd/finly

FROM alpine:3.21
WORKDIR /app
COPY --from=build /finly /app/finly
COPY --from=build /app/frontend/templates /app/frontend/templates
COPY --from=build /app/frontend/static /app/frontend/static
USER 1000:1000
EXPOSE 8080
CMD ["/app/finly"]
