1. Logga in på **Cloudflare** och öppna **{{apex}}**.
2. Gå till **DNS → Records** och klicka **Add record**.
3. **Type:** CNAME, **Name:** `{{name}}`, **Target:** `{{value}}`.
4. Slå av **Proxy status**, så att molnet är **grått (DNS only)**. Vi står för certifikatet; Cloudflares proxy skulle stå i vägen.
5. Klicka **Save**.
