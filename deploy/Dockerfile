# syntax=docker/dockerfile:1

# Build stage: one static binary, no cgo, stripped and trimmed.
FROM golang:1.27 AS build
WORKDIR /src

# Dependencies first so a code change does not re-download them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /go-blogcfc ./cmd/go-blogcfc

# The data directory is created here with the runtime uid so the named
# volume seeds with the right owner (the final image has no shell).
RUN mkdir -p /var/lib/go-blogcfc && chown 65532:65532 /var/lib/go-blogcfc

# Final stage: distroless static, non-root. CA certificates and the time
# zone database come with the base; the binary embeds tzdata as well.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /go-blogcfc /go-blogcfc
COPY --from=build --chown=65532:65532 /var/lib/go-blogcfc /var/lib/go-blogcfc

USER 65532
EXPOSE 8080
ENV PORT=8080 DATA_DIR=/var/lib/go-blogcfc

# No curl in the image: the binary checks itself.
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 \
  CMD ["/go-blogcfc", "healthcheck"]

ENTRYPOINT ["/go-blogcfc"]
CMD ["serve"]
