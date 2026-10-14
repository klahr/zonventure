1. Logga in i **Loopia Kundzon** och öppna **Domäner**.
2. Klicka på **{{apex}}**, sedan **Lägg till subdomän**, och skriv `{{name}}`.
3. Öppna den nya subdomänens **DNS-inställningar** och lägg till en post: typ **CNAME**, värde `{{value}}.` (med punkten på slutet).
4. Ta bort eventuella **A**- eller **AAAA**-poster som Loopia skapat för `{{name}}` och spara.
