FROM golang:1.25-alpine
ENV GOPROXY=https://goproxy.cn,direct
RUN go install github.com/air-verse/air@latest
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
CMD ["air"]
