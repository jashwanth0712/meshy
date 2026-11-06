FROM golang:1.27-alpine AS build
WORKDIR /src
COPY meshy/go.mod meshy/go.sum ./meshy/
RUN cd meshy && go mod download
COPY meshy/ ./meshy/
RUN cd meshy && CGO_ENABLED=0 go build -o /meshy-sync ./cmd/meshy-sync

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /meshy-sync /usr/local/bin/meshy-sync
EXPOSE 8787
ENTRYPOINT ["/usr/local/bin/meshy-sync"]
