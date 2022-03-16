FROM golang:1.14

RUN go get github.com/cespare/reflex

WORKDIR /src
RUN echo "-r '(\.go$|go\.mod)' -s go run ./app/" > /reflex.conf
ENTRYPOINT ["reflex", "-c", "/reflex.conf"]