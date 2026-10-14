1. Logga in där ni köpte **{{apex}}**, eller där dess DNS sköts, och leta upp **DNS**, **DNS-poster** eller **Zonredigerare**.
2. Lägg till en ny post av typen **CNAME**.
3. I namnfältet (ibland *Host* eller *Alias*) skriver ni `{{name}}`. De flesta lägger till {{apex}} själva: skriv inte det.
4. I värdefältet (ibland *Target*, *Pekar på* eller *Data*) skriver ni `{{value}}`.
5. Spara. Säger panelen att `{{name}}` redan finns, ta bort den gamla posten först.
