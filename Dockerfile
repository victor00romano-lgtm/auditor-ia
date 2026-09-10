FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api \
    && CGO_ENABLED=0 go build -o /out/auditor ./cmd/auditor \
    && CGO_ENABLED=0 go build -o /out/auditworker ./cmd/auditworker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build --chown=65532:65532 /out/api /api
COPY --from=build --chown=65532:65532 /out/auditor /auditor
COPY --from=build --chown=65532:65532 /out/auditworker /auditworker
COPY --chown=65532:65532 configs /configs
ENV RULES_FILE=/configs/rules.yaml
USER 65532:65532
ENTRYPOINT ["/api"]
