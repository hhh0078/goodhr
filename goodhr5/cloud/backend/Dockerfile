FROM golang:1.25-alpine
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /app
COPY cloud/backend/go.mod cloud/backend/go.sum ./
COPY local-agent-go-new/go.mod local-agent-go-new/go.sum /local-agent-go-new/
RUN go mod download
COPY cloud/backend/ ./
COPY local-agent-go-new/internal/ /local-agent-go-new/internal/
COPY local-agent-go-new/migrations/ /local-agent-go-new/migrations/
COPY local-agent-go-new/cloudruntime/ /local-agent-go-new/cloudruntime/
CMD ["go", "run", "./cmd/server"]
