FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETOS
ARG TARGETARCH
COPY ${TARGETOS}/${TARGETARCH}/pacer-mcp /pacer-mcp
ENTRYPOINT ["/pacer-mcp"]
