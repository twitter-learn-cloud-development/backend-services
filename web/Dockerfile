# 第一阶段：构建
ARG BUILDER_IMAGE=node:22-alpine
ARG RUNTIME_IMAGE=nginx:alpine
FROM ${BUILDER_IMAGE} AS builder
WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

# 第二阶段：nginx 托管
FROM ${RUNTIME_IMAGE}
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 5556
CMD ["nginx", "-g", "daemon off;"]
