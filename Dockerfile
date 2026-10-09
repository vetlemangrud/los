FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/los ./cmd/los \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/los /los
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["/los"]
