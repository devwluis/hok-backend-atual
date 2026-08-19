# Testar Endpoint POST

## Ação

```bash
curl -s -X POST https://api.imoveischaves.com/agent/task -H "X-Hok-Token: $HOK_TOKEN" -H 'Content-Type: application/json' -d '{"task":"status"}' | python3 -m json.tool
```