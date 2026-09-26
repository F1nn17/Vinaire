# Roadmap Phase 10's single combined image: one Go binary, the frontend's
# static export sitting next to it, no Node/npm in the final layer at all.
# See AGENTS.md's "Rendering model" — the Go backend serves that static
# export as-is (STATIC_DIR, see backend/static.go) from the same port as
# the API; it never runs Next.js or does any HTML templating itself.
#
# Local day-to-day development is unaffected: `npm run dev` and `go run .`
# still run separately with hot reload, exactly as before this file
# existed — this image is only built for a prod/test deploy.

# ---- frontend: `next build` (output: "export") -> frontend/out ----
FROM node:22-alpine AS frontend
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# Same-origin in this image (the Go backend below serves both the API and
# this static export from one port) — relative API calls, no CORS needed.
ENV NEXT_PUBLIC_API_BASE=""
RUN npm run build

# ---- backend: static Go binary ----
FROM golang:1.27-alpine AS backend
WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o /vinaire .

# ---- final: no Node/npm, non-root, just the binary + static files ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=backend /vinaire ./vinaire
COPY --from=frontend /app/out ./web

ENV STATIC_DIR=/app/web
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["./vinaire"]
