---
needs: python3
---
# Tokens
Decode a JWT

## Decode a JWT
Shows what's inside a JSON Web Token. It doesn't check the signature. The token is a secret: it never shows on screen or in your history.

```sh
python3 -I -c 'import sys, json, base64; p = sys.argv[1].split(".")[1]; print(json.dumps(json.loads(base64.urlsafe_b64decode(p + "=" * (-len(p) % 4))), indent=2))' <token>
```

- `<token>` Token
