# 🚀 System Deployment & Verification Guide

## 1. Build & Start Services

Run the following command in the project root to build all Docker images and start the services:

```bash
docker-compose up --build -d
```

Validating service status:

```bash
docker-compose ps
```

Establish dependency order:
`mysql` -> `redis` -> `rabbitmq` -> `consul` -> `services` -> `gateway`

## 2. Verification Steps (Smoke Test)

### 2.1 User Registration & Login

**Register:**
```bash
curl -X POST http://localhost:9638/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"testuser", "email":"test@example.com", "password":"password123"}'
```

**Login:**
```bash
curl -X POST http://localhost:9638/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@example.com", "password":"password123"}'
```
*Save the returned token for subsequent requests.*

### 2.2 Post a Tweet

```bash
curl -X POST http://localhost:9638/api/v1/tweets \
  -H "Authorization: Bearer <TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"content": "Hello World! #golang #microservices"}'
```

### 2.3 Check Trending Topics

```bash
curl http://localhost:9638/api/v1/trends
```
*Should see "golang" and "microservices".*

### 2.4 Upload Media

```bash
curl -X POST http://localhost:9638/api/v1/upload \
  -H "Authorization: Bearer <TOKEN>" \
  -F "file=@./test_image.jpg"
```
*(Make sure to have a `test_image.jpg` or use any file)*

## 3. Observability

- **Consul (Service Discovery):** [http://localhost:8500](http://localhost:8500)
- **Jaeger (Tracing):** [http://localhost:16686](http://localhost:16686)
- **Prometheus (Metrics):** [http://localhost:9090](http://localhost:9090)
- **Grafana (Dashboards):** [http://localhost:3000](http://localhost:3000) (admin/admin)
- **Sentinel (Circuit Breaker):** [http://localhost:8858](http://localhost:8858) (sentinel/sentinel)
