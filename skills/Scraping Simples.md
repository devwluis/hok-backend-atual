# Scraping Simples

## Ação

```bash
curl -s --max-time 10 'https://example.com' | python3 -c "import sys; html=sys.stdin.read(); print(f'Bytes: {len(html)}')"
```
