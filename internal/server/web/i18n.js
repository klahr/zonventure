// Shared i18n module for the three web entry points (play, arrange,
// live). Keeping it as a single embedded script means each HTML page
// stays self-contained in its own <script> block but can still call
// t() without duplicating the dictionary per page.
//
// Convention: the key is the English source string. The English dict
// is empty so t("Log in") returns "Log in" when lang=en; Swedish (and
// future languages) override individual strings by exact key. Puzzle
// content (title, prompt, hint, accept list) is author-provided and is
// intentionally NOT translated — an adventure authored in one language
// renders in that language regardless of the UI locale.
//
// Variable interpolation uses {{name}} placeholders: t("Puzzle {{i}}/{{n}}", {i:1,n:3}).
(function () {
  'use strict';

  const DICT = {
    en: {},
    sv: {
      // ---- brand + login ----
      'arranger': 'arrangör',
      'administrator': 'administratör',
      'Username': 'Användarnamn',
      'Password': 'Lösenord',
      'Log in': 'Logga in',
      'login failed': 'inloggningen misslyckades',
      'Log out ({{username}})': 'Logga ut ({{username}})',
      'Log out': 'Logga ut',
      // ---- nav ----
      'Dashboard': 'Översikt',
      'Organizations': 'Organisationer',
      'Adventures': 'Äventyr',
      'Runs': 'Omgångar',
      'Library': 'Bibliotek',
      'Puzzles': 'Pussel',
      'Members': 'Medlemmar',
      'Admins': 'Administratörer',
      'Use light theme': 'Använd ljust tema',
      'Use dark theme': 'Använd mörkt tema',
      'Use system theme': 'Använd systemets tema',
      'Language': 'Språk',
      // ---- common buttons / words ----
      'Save': 'Spara',
      'Cancel': 'Avbryt',
      'Delete': 'Ta bort',
      'Edit': 'Redigera',
      'Back': 'Tillbaka',
      'Close': 'Stäng',
      'Create': 'Skapa',
      'Remove': 'Ta bort',
      'Confirm': 'Bekräfta',
      'Preview': 'Förhandsvisa',
      'Add': 'Lägg till',
      'Copy': 'Kopiera',
      'Reset': 'Återställ',
      'Rename': 'Byt namn',
      'Renamed': 'Namnet ändrat',
      'Removed': 'Borttagen',
      'Deleted': 'Borttagen',
      'deleted': 'borttagen',
      'Saved': 'Sparat',
      'Assign': 'Tilldela',
      'Revoke': 'Återkalla',
      'Manage': 'Hantera',
      'error': 'fel',
      'error {{status}}': 'fel {{status}}',
      'Loading…': 'Läser in…',
      'loading…': 'läser in…',
      'Not found': 'Hittades inte',
      'not found': 'hittades inte',
      'failed to load': 'kunde inte läsa in',
      'load failed': 'inläsning misslyckades',
      'password required': 'lösenord krävs',
      'Your password': 'Ditt lösenord',
      'Bad date': 'Ogiltigt datum',
      'unknown': 'okänt',
      // ---- preview chrome + debug ----
      'Puzzle {{i}}/{{n}}': 'Pussel {{i}}/{{n}}',
      '◀ Prev': '◀ Föregående',
      'Next ▶': 'Nästa ▶',
      'Debug': 'Felsökning',
      'Debug — author view': 'Felsökning — författarvy',
      'Accepted answers: ': 'Godkända svar: ',
      '(none)': '(inga)',
      'Location: {{coords}}': 'Plats: {{coords}}',
      ' · gate radius {{r}} m': ' · områdesradie {{r}} m',
      ' · not gated': ' · inget område',
      'Notes: ': 'Anteckningar: ',
      'Preview requires an arranger login. Open ':
        'Förhandsvisning kräver arrangörsinloggning. Öppna ',
      ' first, then retry.': ' först och försök sedan igen.',
      // ---- player UI ----
      'Your answer': 'Ditt svar',
      'Submit': 'Skicka',
      'Try an answer (preview)': 'Pröva ett svar (förhandsvisning)',
      'Waiting for the adventure to start…': 'Väntar på att äventyret ska börja…',
      'The arranger paused the run. Stand by.':
        'Arrangören har pausat omgången. Vänta.',
      'Travel to the location': 'Bege dig till platsen',
      'Head within {{r}} m of ({{lat}}, {{lng}}).':
        'Gå inom {{r}} m från ({{lat}}, {{lng}}).',
      'Last fix: {{lat}}, {{lng}} (±{{acc}} m)':
        'Senaste position: {{lat}}, {{lng}} (±{{acc}} m)',
      'Waiting for a GPS fix…': 'Väntar på GPS-position…',
      'Send location now': 'Skicka position nu',
      'Hint: {{text}}': 'Tips: {{text}}',
      'Disconnected — try again in a moment.':
        'Frånkopplad — försök igen om en stund.',
      'Open your magic link': 'Öppna din magiska länk',
      'Players receive a personal link via message from their arranger. ':
        'Spelare får en personlig länk från sin arrangör. ',
      'Open it on this device to join the game.':
        'Öppna den på den här enheten för att delta.',
      // ---- player welcome ----
      'Adventure': 'Äventyr',
      'your team': 'ditt lag',
      'Your team has ': 'Ditt lag har ',
      ' puzzles to solve. Good luck!': ' pussel att lösa. Lycka till!',
      'A few quick tips': 'Några snabba tips',
      'Read each puzzle carefully — the answer is usually a short word or phrase.':
        'Läs varje pussel noga — svaret är oftast ett kort ord eller en kort fras.',
      'Some puzzles only unlock when you reach a specific place. Your device will show a pin and prompt you to go there.':
        'Vissa pussel låses upp först när du kommer till en viss plats. Enheten visar en markör och ber dig gå dit.',
      'The submit button is always in the same spot at the bottom of the screen.':
        'Skicka-knappen finns alltid på samma ställe längst ned på skärmen.',
      'Whoever in your team has this link installed can type answers — pick one phone and keep it handy.':
        'Den i laget som har den här länken kan skicka svar — välj en telefon och ha den nära till hands.',
      'Wrong answers don\'t cost you anything — keep trying.':
        'Fel svar kostar ingenting — fortsätt gissa.',
      'Let\'s go': 'Kör igång',
      // ---- celebrate + rebuff ----
      'Correct!': 'Rätt!',
      'Adventure complete!': 'Äventyret klart!',
      'Adventure complete! 🎉': 'Äventyret klart! 🎉',
      'Next puzzle coming up…': 'Nästa pussel…',
      'Great work — you made it to the end.':
        'Bra jobbat — du kom hela vägen i mål.',
      'Not quite': 'Inte riktigt',
      // ---- player summary ----
      'Loading your run stats…': 'Läser in din statistik…',
      'Could not load stats.': 'Kunde inte läsa in statistik.',
      'Distance': 'Sträcka',
      'Time': 'Tid',
      'Correct answers': 'Rätta svar',
      'Misses': 'Missar',
      'Solved all {{n}} puzzle.': 'Löste alla {{n}} pussel.',
      'Solved all {{n}} puzzles.': 'Löste alla {{n}} pussel.',
      // ---- live (TV) ----
      'Live · push updates · ': 'Direktsändning · push-uppdateringar · ',
      '{{n}} puzzle · ': '{{n}} pussel · ',
      '{{n}} puzzles · ': '{{n}} pussel · ',
      'server time {{time}}': 'servertid {{time}}',
      'Fullscreen': 'Helskärm',
      'Run': 'Omgång',
      'Elapsed': 'Förflutet',
      'Not started': 'Inte startad',
      'No teams yet.': 'Inga lag än.',
      'finished': 'klart',
      'Complete': 'Klart',
      'Puzzle {{i}} of {{n}}': 'Pussel {{i}} av {{n}}',
      'Done': 'Klart',
      'Finish time': 'Sluttid',
      'Solved': 'Lösta',
      'Puzzle': 'Pussel',
      'gated': 'område',
      'Loading map…': 'Läser in karta…',
      'Map library still loading…': 'Kartbiblioteket läser fortfarande in…',
      '{{n}} team on the map': '{{n}} lag på kartan',
      '{{n}} teams on the map': '{{n}} lag på kartan',
      'Waiting for the first position…': 'Väntar på första positionen…',
      // ---- run/team state words (also used by pills in arrange UI) ----
      'running': 'pågår',
      'paused': 'pausad',
      'scheduled': 'schemalagd',
      'completed': 'slutförd',
      'waiting': 'väntar',
      // ---- arrange dashboard ----
      'Hello, {{username}}': 'Hej, {{username}}',
      'You have {{n}} adventure(s) you are allowed to arrange.':
        'Du har {{n}} äventyr som du får arrangera.',
      'Runs total': 'Totalt omgångar',
      'Running now': 'Pågår nu',
      'Scheduled': 'Schemalagda',
      'Recent runs': 'Senaste omgångarna',
      'Signed in as admin — you manage organizations, their assignments, and other admins.':
        'Inloggad som administratör — du hanterar organisationer, deras tilldelningar och övriga administratörer.',
      'Org users': 'Organisationsanvändare',
      'Active runs': 'Aktiva omgångar',
      'No organizations yet.': 'Inga organisationer än.',
      'Create your first organization': 'Skapa din första organisation',
      'Organization': 'Organisation',
      'Users': 'Användare',
      'Players': 'Spelare',
      'Active': 'Aktiva',
      '{{n}} running': '{{n}} pågår',
      ' · {{n}} running': ' · {{n}} pågår',
      // ---- arrange: player-role fallback ----
      'Logged in as {{username}} (player). This dashboard is for arrangers.':
        'Inloggad som {{username}} (spelare). Den här panelen är för arrangörer.',
      'Go to the player view →': 'Gå till spelarvyn →',
      // ---- crumb ----
      '{{section}} / Run #{{id}}': '{{section}} / Omgång #{{id}}',
      '{{section}} / {{slug}}': '{{section}} / {{slug}}',
      // ---- adventures list (arranger) ----
      'Each assignment lets you schedule the adventure once, with up to the number of teams agreed with the owner.':
        'Varje tilldelning låter dig schemalägga äventyret en gång, med upp till det antal lag som avtalats med ägaren.',
      'No assignments available right now.': 'Inga tilldelningar tillgängliga just nu.',
      'Contact the owner to get one.': 'Kontakta ägaren för att få en.',
      'up to {{n}} team': 'upp till {{n}} lag',
      'up to {{n}} teams': 'upp till {{n}} lag',
      'Schedule run': 'Schemalägg omgång',
      'Already used': 'Redan använda',
      'Teams cap': 'Lagtak',
      // ---- adventure detail ----
      '{{slug}} · {{n}} puzzles': '{{slug}} · {{n}} pussel',
      '(untitled)': '(utan titel)',
      'Accepted: ': 'Godkända: ',
      ' · gate {{r}} m': ' · område {{r}} m',
      // ---- schedule modal ----
      'The run is created in the scheduled state. You can start it manually later, or pick a local time here to have the server auto-start it.':
        'Omgången skapas i schemalagt läge. Du kan starta den manuellt senare, eller välja en lokal tid här så startar servern den automatiskt.',
      'Auto-start at (optional)': 'Autostart kl. (valfritt)',
      'Create run': 'Skapa omgång',
      'Run #{{id}} scheduled': 'Omgång #{{id}} schemalagd',
      // ---- runs table ----
      'No runs yet.': 'Inga omgångar än.',
      'State': 'Läge',
      'Started': 'Startad',
      // ---- share bar ----
      'Copy link': 'Kopiera länk',
      'Link copied': 'Länk kopierad',
      'Open ↗': 'Öppna ↗',
      'Map enabled on live view': 'Karta aktiverad i livevyn',
      'Map hidden': 'Karta dold',
      'Show real-time map': 'Visa realtidskarta',
      'Public live view': 'Offentlig livevy',
      'Share on a TV — token URL, no login needed':
        'Visa på tv — token-länk, ingen inloggning behövs',
      // ---- run detail ----
      'Run #{{id}}': 'Omgång #{{id}}',
      'Lifecycle': 'Livscykel',
      'Started {{when}}': 'Startad {{when}}',
      'Scheduled to auto-start {{when}}': 'Schemalagd att autostarta {{when}}',
      'Not started yet': 'Inte startad än',
      'Map': 'Karta',
      'Gated puzzle': 'Områdespussel',
      'New team': 'Nytt lag',
      'Team': 'Lag',
      'done': 'klart',
      'Event log': 'Händelselogg',
      'Map library is still loading — refresh the page.':
        'Kartbiblioteket läser fortfarande in — ladda om sidan.',
      'Gated — {{r}} m': 'Område — {{r}} m',
      // ---- lifecycle actions ----
      'Start now': 'Starta nu',
      'Edit schedule': 'Ändra schema',
      'Pause': 'Pausa',
      'Resume': 'Återuppta',
      'Paused': 'Pausad',
      'Resumed': 'Återupptagen',
      'Start this run now?': 'Starta omgången nu?',
      'Starting the run opens the first puzzle to all teams. ':
        'När omgången startas öppnas det första pusslet för alla lag. ',
      'An auto-start is currently scheduled for ':
        'En autostart är schemalagd till ',
      '. Starting now will replace that.': '. Om du startar nu ersätts den.',
      'You can pause it later if needed.': 'Du kan pausa den senare om det behövs.',
      'Start run': 'Starta omgång',
      'Change auto-start': 'Ändra autostart',
      'Set auto-start': 'Ställ in autostart',
      'The server will flip the run to running automatically at the time below. Leave empty and hit Clear to remove the auto-start.':
        'Servern sätter omgången till pågår automatiskt vid tiden nedan. Lämna tomt och tryck Rensa för att ta bort autostarten.',
      'Auto-start at': 'Autostart kl.',
      'Clear': 'Rensa',
      'Pick a date or use Clear': 'Välj ett datum eller tryck Rensa',
      'Schedule updated': 'Schemat uppdaterat',
      'Auto-start cleared': 'Autostart rensad',
      // ---- team detail / skip ----
      'Team · {{name}}': 'Lag · {{name}}',
      'Finished': 'Klart',
      'Puzzle {{i}}': 'Pussel {{i}}',
      'Skip this puzzle': 'Hoppa över pusslet',
      'Edit members': 'Redigera medlemmar',
      'Activity': 'Aktivitet',
      'Skip puzzle for {{name}}?': 'Hoppa över pusslet för {{name}}?',
      'This advances the team past their current puzzle without them submitting the answer. Useful when they\'re stuck. The action is recorded in the event log.':
        'Det här för laget förbi nuvarande pussel utan att de behöver skicka in något svar. Bra när de har kört fast. Åtgärden loggas.',
      'Skip puzzle': 'Hoppa över pusslet',
      '{{name}} finished': '{{name}} är klart',
      '{{name}} advanced': '{{name}} gick vidare',
      'Delete team “{{name}}”?': 'Ta bort laget ”{{name}}”?',
      'This removes the team and all its progress. Members stay as player accounts. Enter your password to confirm.':
        'Det tar bort laget och all dess framgång. Medlemmarna finns kvar som spelarkonton. Ange ditt lösenord för att bekräfta.',
      'Team deleted': 'Laget borttaget',
      // ---- new team modal ----
      'Team name': 'Lagnamn',
      'Members — one name per line': 'Medlemmar — ett namn per rad',
      'Each line becomes a player slot with its own magic-link token. ':
        'Varje rad blir en spelarplats med en egen magisk länk. ',
      'You can copy the per-player link after the team is created.':
        'Du kan kopiera länken per spelare när laget skapats.',
      'Create team': 'Skapa lag',
      'Add at least one member': 'Lägg till minst en medlem',
      'Team created': 'Laget skapat',
      // ---- edit team modal ----
      'Edit team · {{name}}': 'Redigera lag · {{name}}',
      'Save name': 'Spara namn',
      'No members yet. Add one below.': 'Inga medlemmar än. Lägg till en nedan.',
      'New member name': 'Nytt medlemsnamn',
      'Added {{name}}': '{{name}} tillagd',
      // ---- slot row ----
      'Rotate link': 'Rotera länken',
      'Link rotated': 'Länk roterad',
      'Remove slot': 'Ta bort plats',
      'last login {{when}}': 'senaste inloggning {{when}}',
      // ---- reset password / delete user ----
      'Reset password for {{username}}': 'Återställ lösenord för {{username}}',
      'New password': 'Nytt lösenord',
      'Set password': 'Sätt lösenord',
      'password reset': 'lösenord återställt',
      'Delete {{username}}?': 'Ta bort {{username}}?',
      'This permanently removes {{username}} and unlinks them from any teams. Enter your password to confirm.':
        'Det här tar bort {{username}} permanent och kopplar loss dem från alla lag. Ange ditt lösenord för att bekräfta.',
      // ---- organizations ----
      'Each organization is a tenant — its users (org_admin, org_member) run the games. Assign adventures to an organization to let them schedule runs.':
        'Varje organisation är en tenant — dess användare (org_admin, org_member) driver spelen. Tilldela äventyr till en organisation så att de kan schemalägga omgångar.',
      '{{n}} user': '{{n}} användare',
      '{{n}} users': '{{n}} användare',
      '{{n}} player': '{{n}} spelare',
      '{{n}} players': '{{n}} spelare',
      '{{n}} run': '{{n}} omgång',
      '{{n}} runs': '{{n}} omgångar',
      'Add user': 'Lägg till användare',
      'Users ({{n}})': 'Användare ({{n}})',
      'No users yet — add one to get started.': 'Inga användare än — lägg till en för att komma igång.',
      '{{role}} · click ⓧ to remove': '{{role}} · klicka på ⓧ för att ta bort',
      '{{username}} (admin)': '{{username}} (administratör)',
      '{{username}} (member)': '{{username}} (medlem)',
      'Unused assignments ({{n}})': 'Oanvända tilldelningar ({{n}})',
      'None — assign an adventure to let them schedule a run.':
        'Inga — tilldela ett äventyr så att de kan schemalägga en omgång.',
      'Click to edit': 'Klicka för att redigera',
      '{{slug}} · {{n}} team': '{{slug}} · {{n}} lag',
      '{{slug}} · {{n}} teams': '{{slug}} · {{n}} lag',
      'Used ({{n}})': 'Använda ({{n}})',
      'Click to edit team cap': 'Klicka för att ändra lagtak',
      '{{slug}} · run #{{run}} · max {{max}}': '{{slug}} · omgång #{{run}} · max {{max}}',
      'New organization': 'Ny organisation',
      'Name': 'Namn',
      'Organization created': 'Organisation skapad',
      'Rename {{name}}': 'Byt namn på {{name}}',
      'New name': 'Nytt namn',
      'Delete organization “{{name}}”?': 'Ta bort organisationen ”{{name}}”?',
      'This deletes every user, player, assignment and run that belongs to the organization. Enter your password to confirm.':
        'Det raderar alla användare, spelare, tilldelningar och omgångar som tillhör organisationen. Ange ditt lösenord för att bekräfta.',
      'Organization deleted': 'Organisation borttagen',
      'Add user to {{name}}': 'Lägg till användare i {{name}}',
      'Role': 'Roll',
      'Member': 'Medlem',
      'Admin': 'Administratör',
      'User added': 'Användare tillagd',
      'Remove {{username}}?': 'Ta bort {{username}}?',
      'Removes {{username}} from {{org}}. Enter your password to confirm.':
        'Tar bort {{username}} från {{org}}. Ange ditt lösenord för att bekräfta.',
      'Edit assignment · {{name}}': 'Redigera tilldelning · {{name}}',
      'Adventure (locked — already used)': 'Äventyr (låst — redan använt)',
      'Maximum teams': 'Max antal lag',
      'Linked to run #{{id}}. You may raise the cap; lowering below the current team count is rejected.':
        'Kopplad till omgång #{{id}}. Du kan höja taket; lägre än nuvarande antal lag avvisas.',
      'This assignment has not yet been used.': 'Tilldelningen har inte använts än.',
      'Max teams must be at least 1': 'Max antal lag måste vara minst 1',
      'Assignment updated': 'Tilldelning uppdaterad',
      'No adventures loaded': 'Inga äventyr inlästa',
      'Assign adventure to {{name}}': 'Tilldela äventyr till {{name}}',
      'Each assignment may be used for exactly one run. For more, assign again.':
        'Varje tilldelning kan användas för exakt en omgång. Tilldela igen för fler.',
      'Assignment granted': 'Tilldelning beviljad',
      'Assignment revoked': 'Tilldelning återkallad',
      // ---- puzzles ----
      'Capture a puzzle at your current location': 'Fånga ett pussel på din nuvarande plats',
      'The pool of every puzzle in the system. Unassigned ones sit in the Library; the rest list the adventure they belong to. Copy a puzzle into a new adventure from the adventure editor.':
        'Samlingen av alla pussel i systemet. Otilldelade ligger i biblioteket; övriga visar vilket äventyr de hör till. Kopiera ett pussel till ett nytt äventyr från äventyrseditorn.',
      'No puzzles yet.': 'Inga pussel än.',
      'Capture one at your location': 'Fånga ett på din plats',
      'Puzzle captured': 'Pussel fångat',
      'Assigned to adventure': 'Tilldelad äventyr',
      'library': 'bibliotek',
      '(no content yet)': '(inget innehåll än)',
      'disabled': 'avstängd',
      ' · has images': ' · har bilder',
      'puzzle not found': 'pusslet hittades inte',
      'Back to puzzles': 'Tillbaka till pussel',
      'Opening editor…': 'Öppnar editorn…',
      // ---- library ----
      'New adventure': 'Nytt äventyr',
      'Adventures you have authored live in the database. Assign them to organizations from the Organizations page.':
        'Äventyr du har skapat lever i databasen. Tilldela dem till organisationer från sidan Organisationer.',
      'No adventures yet.': 'Inga äventyr än.',
      'Create your first adventure': 'Skapa ditt första äventyr',
      '{{n}} puzzles': '{{n}} pussel',
      'Use default': 'Använd standard',
      '(default)': '(standard)',
      'Accent — buttons, progress': 'Accent — knappar, framsteg',
      'Secondary — titles, status pill': 'Sekundär — rubriker, statusmärke',
      'Background': 'Bakgrund',
      'Text': 'Text',
      'Slug — lowercase letters, digits, hyphens': 'Slug — små bokstäver, siffror, bindestreck',
      'Title': 'Titel',
      'Description': 'Beskrivning',
      'Author': 'Författare',
      'Brand colours (optional)': 'Varumärkesfärger (valfritt)',
      'Adventure created': 'Äventyret skapat',
      'Back to library': 'Tillbaka till biblioteket',
      'Open the adventure in the player view': 'Öppna äventyret i spelarvyn',
      'Edit title/slug': 'Redigera titel/slug',
      'Puzzles ({{n}})': 'Pussel ({{n}})',
      'From library': 'Från biblioteket',
      'New puzzle': 'Nytt pussel',
      'Edit adventure': 'Redigera äventyr',
      'Slug': 'Slug',
      'Brand colours': 'Varumärkesfärger',
      'Delete adventure “{{title}}”?': 'Ta bort äventyret ”{{title}}”?',
      'This removes the adventure and all its puzzles. Enter your password to confirm.':
        'Det här tar bort äventyret och alla dess pussel. Ange ditt lösenord för att bekräfta.',
      'Open in the player view with author debug panel':
        'Öppna i spelarvyn med författarens felsökningspanel',
      // ---- puzzle editor ----
      'Library: {{title}}': 'Bibliotek: {{title}}',
      'Library puzzle': 'Bibliotekspussel',
      'Puzzle title': 'Pusslets titel',
      'What the player sees…': 'Det spelaren ser…',
      '(optional hint)': '(valfritt tips)',
      'Image removed': 'Bild borttagen',
      'Click to add image': 'Klicka för att lägga till bild',
      'Save the puzzle first, then add images':
        'Spara pusslet först, lägg sedan till bilder',
      'Image added': 'Bild tillagd',
      'Player answer bar (preview)': 'Spelarens svarsrad (förhandsvisning)',
      'Hint:': 'Tips:',
      'hello, hi': 'hej, tjena',
      'e.g. "statue at the fountain, check opening hours"':
        't.ex. ”statyn vid fontänen, kolla öppettider”',
      'Use this device\'s GPS': 'Använd enhetens GPS',
      'Use my location': 'Använd min plats',
      'Settings': 'Inställningar',
      'Accepted answers — comma-separated': 'Godkända svar — kommaseparerade',
      'Internal notes — not shown to players': 'Interna anteckningar — visas inte för spelarna',
      'Location': 'Plats',
      'Drag the pin, click the map, or press the button':
        'Dra markören, klicka på kartan eller tryck på knappen',
      'Latitude': 'Latitud',
      'Longitude': 'Longitud',
      'Gated — player must reach location first':
        'Område — spelaren måste nå platsen först',
      'Gate radius (metres)': 'Områdesradie (meter)',
      'Disabled — players skip this puzzle; the content and gate stay saved':
        'Avstängd — spelarna hoppar över pusslet; innehåll och område sparas ändå',
      'Puzzle created': 'Pussel skapat',
      'Puzzle saved': 'Pusslet sparat',
      'Discard unsaved changes in this puzzle?':
        'Kasta osparade ändringar i det här pusslet?',
      'Puzzle {{i}} (untitled)': 'Pussel {{i}} (utan titel)',
      ' — disabled': ' — avstängd',
      'This puzzle': 'Det här pusslet',
      'New': 'Nytt',
      '📚 Library puzzle — not yet in any adventure':
        '📚 Bibliotekspussel — ännu inte i något äventyr',
      'In adventure ': 'I äventyret ',
      ', position ': ', position ',
      'Geolocation not available on this device': 'Platstjänst saknas på den här enheten',
      'GPS failed: {{msg}}': 'GPS misslyckades: {{msg}}',
      // ---- library picker ----
      'Pick a puzzle from the library': 'Välj ett pussel från biblioteket',
      'Loading puzzles…': 'Läser in pussel…',
      'No puzzles in the library yet.': 'Inga pussel i biblioteket än.',
      'in {{title}} · #{{n}}': 'i {{title}} · #{{n}}',
      'Puzzle copied into adventure': 'Pussel kopierat till äventyret',
      // ---- delete puzzle ----
      'Delete puzzle {{n}}?': 'Ta bort pussel {{n}}?',
      'This removes the puzzle and any images attached to it.':
        'Det här tar bort pusslet och alla bilder som hör till det.',
      // ---- admins ----
      'New admin': 'Ny administratör',
      'Global admins manage organizations and other admins. The system will refuse to let you delete the last remaining admin.':
        'Globala administratörer hanterar organisationer och andra administratörer. Systemet låter dig inte ta bort den sista administratören.',
      'No admins yet.': 'Inga administratörer än.',
      '(you)': '(du)',
      'Admin created': 'Administratör skapad',
      'Delete admin {{username}}?': 'Ta bort administratör {{username}}?',
      'Enter your password to confirm.': 'Ange ditt lösenord för att bekräfta.',
      'Admin deleted': 'Administratör borttagen',
      // ---- members (org admin) ----
      'No organization.': 'Ingen organisation.',
      'Your organization': 'Din organisation',
      'Users in {{name}}. Admins can invite more users; members run the games.':
        'Användare i {{name}}. Administratörer kan bjuda in fler användare; medlemmar driver spelen.',
      'No other users yet.': 'Inga andra användare än.',
      'admin': 'administratör',
      'member': 'medlem',
      // ---- event feed ----
      'No activity yet.': 'Ingen aktivitet än.',
      'Run created for ': 'Omgång skapad för ',
      'Run started': 'Omgången startade',
      'Run paused': 'Omgången pausades',
      'Run resumed': 'Omgången återupptogs',
      'Team ': 'Laget ',
      ' created': ' skapades',
      'with {{names}}': 'med {{names}}',
      'Renamed ': 'Döpte om ',
      ' deleted': ' togs bort',
      'Added ': 'La till ',
      ' to ': ' till ',
      'Removed ': 'Tog bort ',
      ' from ': ' från ',
      ' wrong on puzzle {{n}}': ' svarade fel på pussel {{n}}',
      'submitted “{{answer}}”': 'skickade ”{{answer}}”',
      ' solved puzzle {{n}}': ' löste pussel {{n}}',
      ' finished the adventure!': ' klarade äventyret!',
      ' skipped past puzzle {{n}}': ' hoppade förbi pussel {{n}}',
      'by arranger': 'av arrangören',
      ' reached the puzzle location': ' nådde pusselplatsen',
      'by {{actor}}': 'av {{actor}}',
      'just now': 'just nu',
      '{{n}}m ago': '{{n}}m sedan',
      '{{n}}h ago': '{{n}}h sedan',
    },
  };

  const LANG_LABEL = { en: 'English', sv: 'Svenska' };

  function available() {
    return Object.keys(DICT);
  }

  function pickInitial() {
    const saved = localStorage.getItem('zv-lang');
    if (saved && DICT[saved]) return saved;
    const browser = (navigator.language || '').slice(0, 2).toLowerCase();
    return DICT[browser] ? browser : 'en';
  }

  let currentLang = pickInitial();
  document.documentElement.setAttribute('lang', currentLang);

  function t(key, vars) {
    const table = DICT[currentLang] || {};
    let s = table[key];
    if (s === undefined) s = key; // fall through to the English source text
    if (vars) {
      for (const k of Object.keys(vars)) {
        s = s.replace(new RegExp('\\{\\{\\s*' + k + '\\s*\\}\\}', 'g'), vars[k]);
      }
    }
    return s;
  }

  function setLang(code) {
    if (!DICT[code] || code === currentLang) return;
    currentLang = code;
    localStorage.setItem('zv-lang', code);
    // Full reload is the simplest way to re-run every t() call across
    // the app. The login/localStorage session survives the reload.
    location.reload();
  }

  function getLang() { return currentLang; }

  // makeLangPicker returns a <select> pre-populated with the available
  // languages. Styling is up to the caller — the sidebar footer adds
  // the "ghost"-style classes to match its siblings.
  function makeLangPicker(opts = {}) {
    const sel = document.createElement('select');
    sel.className = opts.className || 'zv-lang-picker';
    if (opts.title) sel.title = opts.title;
    for (const code of available()) {
      const o = document.createElement('option');
      o.value = code;
      o.textContent = LANG_LABEL[code] || code;
      if (code === currentLang) o.selected = true;
      sel.append(o);
    }
    sel.addEventListener('change', () => setLang(sel.value));
    return sel;
  }

  window.zvI18n = { t, setLang, getLang, available, makeLangPicker };
  // Shorthand so call sites read cleanly: t('Save')
  window.t = t;
})();
