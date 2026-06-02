FROM gcr.io/distroless/static-debian12:nonroot
COPY pacer-mcp /pacer-mcp
ENTRYPOINT ["/pacer-mcp"]
