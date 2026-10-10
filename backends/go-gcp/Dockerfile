FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY libs/devjwt /src/libs/devjwt
COPY libs/merge /src/libs/merge
COPY backends/go-gcp /src/backends/go-gcp
WORKDIR /src/backends/go-gcp
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
COPY contract/fixtures /fixtures
ENV FIXTURES_DIR=/fixtures PORT=8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/api"]
