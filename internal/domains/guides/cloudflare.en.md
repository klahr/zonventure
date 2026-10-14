1. Sign in to **Cloudflare** and open **{{apex}}**.
2. Go to **DNS → Records** and click **Add record**.
3. **Type:** CNAME, **Name:** `{{name}}`, **Target:** `{{value}}`.
4. Switch **Proxy status** off, so the cloud is **grey (DNS only)**. We provide the certificate; Cloudflare's proxy would stand in the way.
5. Click **Save**.
