FROM gcr.io/distroless/static-debian12:nonroot
ARG TARGETOS
ARG TARGETARCH
LABEL io.modelcontextprotocol.server.name="io.github.STR-Consulting/pacer-mcp"
COPY ${TARGETOS}/${TARGETARCH}/pacer-mcp /pacer-mcp
ENTRYPOINT ["/pacer-mcp"]
