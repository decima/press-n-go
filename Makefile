# Makefile for the Press-n-Go application

# Default version is 'latest'. Override from the command line.
# Example: make build VERSION=1.0.0
VERSION ?= latest

# The name of the Docker image.
IMAGE_NAME := decima/press-n-go

# .PHONY declares targets that are not files.
.PHONY: all build run push

all: build

# Build the Docker image with the specified name and version tag.
build:
	@echo "Building Docker image $(IMAGE_NAME):$(VERSION)..."
	@docker build -t $(IMAGE_NAME):$(VERSION) .

# Run the Docker container.
# You can pass credentials directly.
# Example: make run USER=admin PASS=secret PORT=9000
run:
	@echo "Running container from image $(IMAGE_NAME):$(VERSION)..."
	@docker run --rm -it -p ${PORT_MAP:-8080}:${PORT:-8080} \
		-e PORT=${PORT:-8080} \
		-e PNG_USERNAME=${USER} \
		-e PNG_PASSWORD=${PASS} \
		$(IMAGE_NAME):$(VERSION)

# Push the Docker image to a container registry (e.g., Docker Hub).
# You must be logged in to the registry first (`docker login`).
push:
	@echo "Pushing $(IMAGE_NAME):$(VERSION) to registry..."
	@docker push $(IMAGE_NAME):$(VERSION)

