# Checar SSL

## Ação

```bash
echo | openssl s_client -connect api.imoveischaves.com:443 2>/dev/null | openssl x509 -noout -dates 2>/dev/null
```
