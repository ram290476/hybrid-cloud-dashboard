FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY libs/devjwt /src/libs/devjwt
COPY facade /src/facade
WORKDIR /src/facade
RUN CGO_ENABLED=0 go build -o /out/facade ./cmd/facade

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/facade /facade
COPY facade/policy /policy
COPY frontend /frontend
ENV ROUTING_POLICY=/policy/routing-policy.kind.yaml PORT=8080 FRONTEND_DIR=/frontend
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/facade"]
