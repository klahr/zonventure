1. Sign in to **Loopia Customer zone** and open **Domains**.
2. Click **{{apex}}**, then **Add subdomain**, and enter `{{name}}`.
3. Open the new subdomain's **DNS settings** and add a record: type **CNAME**, value `{{value}}.` (with the dot at the end).
4. Remove any **A** or **AAAA** record Loopia created for `{{name}}`, then save.
