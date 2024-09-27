# Step 1: Use the official Golang image to build the app
FROM golang:1.20-alpine AS build

# Step 2: Set the current working directory inside the container
WORKDIR /app

# Step 3: Copy the Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Step 4: Copy the rest of the application source code
COPY main.go .

# Step 5: Build the Go app
RUN go build -o slackbot .

# Step 6: Use a minimal base image to run the app
FROM alpine:latest

# Step 7: Set the working directory
WORKDIR /app

# Step 8: Copy the built binary from the builder container
COPY --from=build /app/slackbot /app/slackbot

# Step 9: Copy the configuration file
COPY config.json /app/

# Step 10: Expose the port the app runs on
EXPOSE 8081

# Step 11: Command to run the executable
CMD ["./slackbot"]
