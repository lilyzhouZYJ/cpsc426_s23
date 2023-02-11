# syntax=docker/dockerfile:1

# Include the official Go image as the base image
FROM golang:1.19-alpine

# Create a directory inside the image we are building;
# Also sets this dirctory as the default destination for all subsequent commands
WORKDIR /vrservice

# Copy go.mod and go.sum into WORKDIR
COPY go.mod ./
COPY go.sum ./

# Install the Go modules into the WORKDIR in the image
RUN go mod download

# Copy source code
# COPY ./ ./lab2
ADD ./det_bcrypt ./det_bcrypt
ADD ./logging ./logging
ADD ./failure_injection ./failure_injection
ADD ./ranker ./ranker
ADD ./video_rec_service ./video_rec_service
ADD ./user_service ./user_service
ADD ./video_service ./video_service
COPY ./Makefile ./

# Create static application binary
RUN cd ./video_rec_service/server && go build -o /video_rec_service

EXPOSE 8080

# Tell Docker what command to execute when our image is used to start a container
CMD [ "/video_rec_service" ]