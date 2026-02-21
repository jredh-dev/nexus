# Build stage
# Build context is the repo root (docker build -f services/web/Dockerfile .)
FROM node:22-alpine AS build

WORKDIR /app

# Copy only web service package files first for layer caching
COPY services/web/package.json services/web/package-lock.json* ./services/web/
RUN cd services/web && npm ci

# Copy the web service source
COPY services/web/ ./services/web/

# Build-time env: PUBLIC_API_URL stays as '/' since we proxy through SSR middleware
ARG PUBLIC_API_URL=/
ENV PUBLIC_API_URL=${PUBLIC_API_URL}

RUN cd services/web && npm run build

# Runtime stage
FROM node:22-alpine AS runtime

WORKDIR /app

COPY --from=build /app/services/web/dist ./dist
COPY --from=build /app/services/web/node_modules ./node_modules

RUN chown -R node:node /app

USER node

ENV HOST=0.0.0.0
ENV PORT=8080
# Portal backend URL for SSR proxy (service-to-service within Cloud Run)
ENV PORTAL_URL=http://localhost:8080

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/ || exit 1

CMD ["node", "dist/server/entry.mjs"]
