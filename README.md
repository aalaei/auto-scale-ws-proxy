# Auto-scale-ws-proxy

A smart WebSocket reverse proxy for V2Ray (vmess) that auto-scales a Kubernetes backend deployment based on activity.

## 🚀 Usage

### Run with Go

```bash
go run auto-scale-ws-proxy.go
```
### Or use Docker
```bash
docker build -t auto-scale-ws-proxy .
docker run -e SECRET_PATH=/vmessws \
           -e BACKEND_URL=http://127.0.0.1:3001 \
           -e KUBE_CLUSTER_TOKEN=... \
           -e KUBE_CLUSTER_ENDPOINT=... \
           auto-scale-ws-proxy
```
| Variable                | Description                      | Default                  |
|-------------------------|---------------------------------|--------------------------|
| `LISTEN_ADDR`           | Address to listen on             | `:8080`                  |
| `SECRET_PATH`           | Path to receive WebSocket        | `/vmessws`               |
| `BACKEND_URL`           | Backend service URL              | `http://127.0.0.1:3001`  |
| `BACKEND_PATH`          | Backend WebSocket Path           | `/ws`                    |
| `KUBE_CLUSTER_ENDPOINT` | Kubernetes API endpoint          | *(required)*             |
| `KUBE_CLUSTER_TOKEN`    | Bearer token for Kubernetes auth| *(required)*             |
| `NAMESPACE`             | Kubernetes namespace             | `test`                   |
| `DEPLOYMENT_NAME`       | Kubernetes deployment name       | `t2`                     |
| `INACTIVITY_MINUTES`    | Minutes before scale-down        | `60`                     |


---

## License

This project is licensed under the GNU General Public License v3.0 (GPL-3.0). See the [LICENSE](LICENSE) file for details.
