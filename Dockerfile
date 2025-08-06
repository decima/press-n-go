FROM golang:1.24-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum to leverage Docker's layer caching.
# Dependencies are downloaded only when these files change.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Build the Go application into a static binary.
# CGO_ENABLED=0 is crucial for creating a static binary that can run on a minimal base image.
# -ldflags="-w -s" strips debug information, making the binary smaller.
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o /press-n-go .

# ---

# Stage 2: Create the final, minimal production image
FROM alpine:latest

# It's a security best practice to run containers as a non-root user.
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser

WORKDIR /app

# Copy the compiled binary from the builder stage
COPY --from=builder /press-n-go .

# Copy the templates directory. The application needs this to render the homepage.
COPY templates ./templates

# The application creates the 'public' directory at runtime,
# but creating it here ensures the correct permissions are set for our non-root user.
RUN mkdir public

# Set the default port. This can be overridden by the '-e PORT=<port>' flag when running the container.
ENV PORT 8080

# Expose the port that the application will listen on.
EXPOSE 8080

# The command to run when the container starts.
ENTRYPOINT ["./press-n-go"]
