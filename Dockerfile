FROM golang AS build

ENV pkg=github.com/ImJasonH/kontain

RUN mkdir -p /go/src/$pkg
ADD . /go/src/$pkg
WORKDIR /go/src/$pkg
ENV GOPATH=/go
RUN go build -o main .

FROM golang
COPY --from=build /go/src/github.com/ImJasonH/kontain/main /app

ENTRYPOINT ["/app"]

