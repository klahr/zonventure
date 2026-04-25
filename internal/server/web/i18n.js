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
      'Store': 'Butik',
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
      'Publish': 'Publicera',
      'Unpublish': 'Avpublicera',
      'Published': 'Publicerad',
      'Unpublished': 'Avpublicerad',
      'Draft': 'Utkast',
      'Hide from the Store': 'Dölj från butiken',
      'Make available in the Store': 'Gör tillgänglig i butiken',
      'Shared': 'Delat',
      'Shared with organisations': 'Delat med organisationer',
      'Give specific organisations free access to this adventure without publishing it to the Store.':
        'Ge specifika organisationer fri åtkomst till detta äventyr utan att publicera det i butiken.',
      'Not shared with any organisation yet.': 'Inte delat med någon organisation ännu.',
      'Share with org': 'Dela med organisation',
      'Share with organisation': 'Dela med organisation',
      'Pick one. The organisation can schedule runs of this adventure without buying team slots.':
        'Välj en. Organisationen kan schemalägga omgångar av detta äventyr utan att köpa lagplatser.',
      'Revoke share': 'Återkalla delning',
      'Every organisation already has access.': 'Alla organisationer har redan åtkomst.',
      'Revoke share with {{org}}? Existing runs keep working; new runs will be blocked.':
        'Återkalla delning med {{org}}? Pågående omgångar fortsätter, men nya omgångar blockeras.',
      'Share revoked': 'Delning återkallad',
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
      // ---- hint system ----
      'Hints': 'Tips',
      'Hint': 'Tips',
      'Hint {{i}}/{{n}}': 'Tips {{i}}/{{n}}',
      'Tap below to reveal your first hint. Use them sparingly — you can always come back.':
        'Tryck nedan för att se ditt första tips. Använd dem sparsamt — du kan alltid komma tillbaka.',
      'Reveal hint': 'Visa tips',
      'Reveal next hint': 'Visa nästa tips',
      "The next hint isn't ready yet. Keep trying for a little longer.":
        'Nästa tips är inte redo ännu. Kämpa på ett tag till.',
      // ---- hint editor (arrange) ----
      'Add hint': 'Lägg till tips',
      'Remove hint': 'Ta bort tips',
      'Clue in markdown…': 'Ledtråd i markdown…',
      'No hints yet — add one below for a delayed helper button.':
        'Inga tips ännu — lägg till ett nedan så dyker en hjälpknapp upp efter en stund.',
      'Unlock after': 'Lås upp efter',
      's': 's',
      '(immediate)': '(direkt)',
      'Each hint unlocks a configurable number of seconds after the team reaches this puzzle. Players reveal them one at a time with a button.':
        'Varje tips låses upp ett konfigurerbart antal sekunder efter att laget når detta pussel. Spelare visar dem ett i taget med en knapp.',
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
      'Shake': 'Skaka',
      'Shuffle ranks + jitter positions (d)': 'Blanda placeringar + skaka positioner (d)',
      'Debug: shake things up': 'Felsök: skaka om',
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
      'Getting started': 'Kom igång',
      'A few clicks to your first adventure.': 'Några klick till ditt första äventyr.',
      'Pick an adventure — browse the Store or create your own':
        'Välj ett äventyr — bläddra i butiken eller skapa ett eget',
      'Have team slots ready — an assignment lets you schedule a run':
        'Ha lagplatser redo — en tilldelning låter dig schemalägga en omgång',
      'Schedule a run, add teams, share their links — and start!':
        'Schemalägg en omgång, lägg till lag, dela länkarna — och kör!',
      'Schedule': 'Schemalägg',
      'Go': 'Kör',
      'Start a new event': 'Starta ett nytt event',
      'Unused assignments': 'Oanvända tilldelningar',
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
      'No adventures you can run yet.': 'Inga äventyr du kan köra än.',
      'Create your own or browse the Store for published adventures.':
        'Skapa ett eget eller bläddra i butiken efter publicerade äventyr.',
      'Browse the Store': 'Bläddra i butiken',
      'Yours': 'Eget',
      '{{n}} puzzle': '{{n}} pussel',
      '{{n}} assignment available': '{{n}} tilldelning tillgänglig',
      '{{n}} assignments available': '{{n}} tilldelningar tillgängliga',
      '{{n}} run held': '{{n}} omgång bokad',
      '{{n}} runs held': '{{n}} omgångar bokade',
      'Pick an assignment': 'Välj en tilldelning',
      'You hold several assignments for {{title}}. Which one do you want to schedule?':
        'Du har flera tilldelningar för {{title}}. Vilken vill du schemalägga?',
      'up to {{n}} teams · purchased {{when}}':
        'upp till {{n}} lag · köpt {{when}}',
      // Legacy strings kept only for translators who might still hit
      // them via old links; the main Adventures page no longer uses
      // the Assignments block.
      'Each assignment lets you schedule the adventure once, with up to the number of teams agreed with the owner.':
        'Varje tilldelning låter dig schemalägga äventyret en gång, med upp till det antal lag som avtalats med ägaren.',
      'No assignments available right now.': 'Inga tilldelningar tillgängliga just nu.',
      'Contact the owner to get one.': 'Kontakta ägaren för att få en.',
      'up to {{n}} team': 'upp till {{n}} lag',
      'up to {{n}} teams': 'upp till {{n}} lag',
      'Schedule run': 'Schemalägg omgång',
      'Already used': 'Redan använda',
      'Teams cap': 'Lagtak',
      // ---- store ----
      'Published adventures from other organisations. Buy team slots to add one to your own Adventures list.':
        'Publicerade äventyr från andra organisationer. Köp lagplatser för att lägga till ett i din egen Äventyr-lista.',
      'No published adventures yet.': 'Inga publicerade äventyr än.',
      '(no description)': '(ingen beskrivning)',
      'Buy team slots': 'Köp lagplatser',
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
      'Simulator': 'Simulator',
      'Running': 'Kör',
      'Idle': 'Vilande',
      'Start': 'Starta',
      'Running for {{t}}': 'Kör sedan {{t}}',
      'Open live view': 'Öppna livevy',
      'Fake a race so you can see the live view animate without recruiting real players. Writes to the same tables as a real run, so every subscriber (map, leaderboard, WS) updates as if players were on the ground.':
        'Fejka en tävling så att du kan se livevyn animera utan att värva riktiga spelare. Skriver till samma tabeller som en riktig omgång, så alla prenumeranter (karta, topplista, WS) uppdateras som om spelarna var på plats.',
      'Configuration': 'Inställningar',
      'Teams (2–20)': 'Lag (2–20)',
      'Speed multiplier (1–50)': 'Hastighetsmultiplikator (1–50)',
      'Higher speed advances the walk faster without changing the ping cadence, so fast-forwarded runs still look natural.':
        'Högre hastighet snabbar på vandringen utan att ändra ping-frekvensen, så snabbspolade omgångar ser naturliga ut.',
      '(no adventures)': '(inga äventyr)',
      'Simulation started': 'Simulering startad',
      'Simulation reset': 'Simulering återställd',
      'Network error — is the server running?': 'Nätverksfel — kör servern?',
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
      // ---- event/run/team workflow (arranger) ----
      '—': '—',
      '(no time set)': '(ingen tid satt)',
      '{{i}} / {{n}}': '{{i}} / {{n}}',
      'Hide': 'Dölj',
      'Show': 'Visa',
      'When': 'När',
      'Date': 'Datum',
      'Total': 'Totalt',
      'Total: ': 'Totalt: ',
      'Slots': 'Platser',
      'Teams': 'Lag',
      'yours': 'eget',
      'catalog': 'butik',
      'No matches.': 'Inga träffar.',
      'stuck?': 'fast?',
      'Filter by title, notes or adventure': 'Filtrera på titel, anteckning eller äventyr',
      'Not scheduled': 'Inte schemalagd',
      'Auto-start {{when}}': 'Autostart {{when}}',
      'Pick an adventure': 'Välj ett äventyr',
      'Pick an auto-start time': 'Välj en autostarttid',
      'Starts soon': 'Börjar snart',
      // ---- team dialogs ----
      'Add teams': 'Lägg till lag',
      'Add the first team': 'Lägg till första laget',
      'Team capacity': 'Lagkapacitet',
      'Team slots': 'Lagplatser',
      'Team name required': 'Lagnamn krävs',
      'Team name prefix': 'Lagnamnsprefix',
      'Player name prefix': 'Spelarnamnsprefix',
      'Players per team': 'Spelare per lag',
      'Number of teams': 'Antal lag',
      'One team': 'Ett lag',
      'Many teams at once': 'Flera lag samtidigt',
      'Name (optional)': 'Namn (valfritt)',
      'Members — one per line, or paste CSV': 'Medlemmar — en per rad, eller klistra in CSV',
      'Each line becomes a player slot with its own magic-link token.':
        'Varje rad blir en spelarplats med sin egen magiska länk-token.',
      'Creates {{t}} teams × {{p}} players = {{n}} magic links.':
        'Skapar {{t}} lag × {{p}} spelare = {{n}} magiska länkar.',
      'Teams (optional — add later if you prefer)':
        'Lag (valfritt — lägg till senare om du vill)',
      'Created {{n}} teams': 'Skapade {{n}} lag',
      'Created {{c}}, {{f}} failed': 'Skapade {{c}}, {{f}} misslyckades',
      'Created {{c}}, {{f}} failed: {{err}}': 'Skapade {{c}}, {{f}} misslyckades: {{err}}',
      'Could not create any teams': 'Kunde inte skapa några lag',
      'Edit team': 'Redigera lag',
      'Delete team': 'Ta bort lag',
      'Open team page': 'Öppna lagets sida',
      'Copy team share link': 'Kopiera lagets delningslänk',
      'Team link copied': 'Laglänk kopierad',
      'Share with the team': 'Dela med laget',
      'Send this single link to the captain. Each member taps their own name there to pick up their personal magic link.':
        'Skicka den här länken till kaptenen. Varje medlem trycker på sitt namn där för att hämta sin personliga magiska länk.',
      'Copy all as text': 'Kopiera allt som text',
      'Copied {{n}} links': 'Kopierade {{n}} länkar',
      'Clipboard blocked — select and copy by hand':
        'Urklipp blockerat — markera och kopiera manuellt',
      'Rotate link — invalidates the old one':
        'Rotera länk — ogiltigförklarar den gamla',
      'Alt-click to skip the confirmation': 'Alt-klicka för att hoppa över bekräftelsen',
      // ---- run lifecycle ----
      'New run': 'Ny omgång',
      'Rename run': 'Byt namn på omgång',
      'Run name': 'Omgångens namn',
      'Run name (optional)': 'Omgångens namn (valfritt)',
      'Run ready': 'Omgången är klar',
      'Run creation failed': 'Kunde inte skapa omgång',
      'Empty resets to "Run #{{id}}".': 'Tomt återställer till "Omgång #{{id}}".',
      'Reschedule': 'Schemalägg om',
      'Schedule for later': 'Schemalägg för senare',
      'Download results': 'Ladda ner resultat',
      'Download standings': 'Ladda ner ställning',
      'Download CSV of the current standings': 'Ladda ner CSV med nuvarande ställning',
      // ---- event wizard ----
      'Create event': 'Skapa event',
      'Event ready': 'Eventet är klart',
      'Create or browse an adventure first': 'Skapa eller bläddra i ett äventyr först',
      'Assign adventure': 'Tilldela äventyr',
      'You have an unused assignment for this adventure — no new purchase needed.':
        'Du har en oanvänd tilldelning för detta äventyr — inget nytt köp behövs.',
      'Each team slot lets one team play "{{title}}".':
        'Varje lagplats låter ett lag spela "{{title}}".',
      // ---- purchases ----
      'Purchases': 'Köp',
      'Purchase failed': 'Köpet misslyckades',
      'Payment failed': 'Betalningen misslyckades',
      'Payment received — let\'s schedule your run.':
        'Betalning mottagen — dags att schemalägga din omgång.',
      'Proceed to payment': 'Fortsätt till betalning',
      'How many team slots do you need? ($2.00 each, simulated payment.)':
        'Hur många lagplatser behöver du? ($2,00 styck, simulerad betalning.)',
      'Browse catalog': 'Bläddra i butiken',
      'No purchases yet.': 'Inga köp än.',
      'Every team-slot purchase. Paid ones produce an assignment you can schedule from the Adventures page.':
        'Alla lagplatsköp. Betalda ger en tilldelning som du kan schemalägga från Äventyr-sidan.',
      '({{price}} per slot)': '({{price}} per plats)',
      '{{n}} team slot': '{{n}} lagplats',
      '{{n}} team slots': '{{n}} lagplatser',
      // ---- adventure delete / edit meta ----
      'Delete adventure': 'Ta bort äventyr',
      'Edit title/slug/colours': 'Redigera titel/slug/färger',
      'e.g. “Saturday Andersson party”': 't.ex. ”Lördagens Andersson-kalas”',
      // ---- team avatar ----
      'Team photo': 'Lagfoto',
      'Upload photo': 'Ladda upp foto',
      'Remove photo': 'Ta bort foto',
      'Team photo updated': 'Lagfotot uppdaterat',
      'Team photo removed': 'Lagfotot borttaget',
      // ---- adventure cover ----
      'Cover picture': 'Omslagsbild',
      'Upload cover': 'Ladda upp omslag',
      'Remove cover': 'Ta bort omslag',
      'Cover updated': 'Omslaget uppdaterat',
      'Cover removed': 'Omslaget borttaget',
      // ---- no-answer puzzles ----
      'Continue': 'Fortsätt',
      "Check location · I'm here": 'Kontrollera plats · Jag är här',
      // ---- server logs ----
      // Note: 'Pause' / 'Resume' / 'Clear' / 'Show' are already
      // translated above for the run lifecycle / modal controls.
      'Logs': 'Loggar',
      'Loading terminal…': 'Läser in terminal…',
      'Filter (substring)': 'Filter (delsträng)',
      'Server log level': 'Loggnivå på servern',
      'Client-side minimum level': 'Minsta loggnivå i klienten',
      'Server level': 'Servernivå',
      // ---- audit log ----
      'Audit': 'Granskning',
      'Login, set-password, magic-link and logout events. Newest first.':
        'Inloggning, lösenordssättning, magisk länk och utloggning. Nyaste först.',
      'Time': 'Tid',
      'Event': 'Händelse',
      'Result': 'Resultat',
      'User': 'Användare',
      'IP': 'IP',
      'Reason': 'Orsak',
      'ok': 'ok',
      'fail': 'fel',
      'Load more': 'Hämta fler',
      'No events recorded.': 'Inga händelser registrerade.',
      'End of log.': 'Slut på loggen.',
      // ---- magic-link expiry ----
      'expired': 'utgången',
      'expires in {{n}}d': 'går ut om {{n}}d',
      'Expired {{when}}': 'Gick ut {{when}}',
      'Expires {{when}}': 'Går ut {{when}}',
      'Last opened {{when}}': 'Senast öppnad {{when}}',
      // ---- signup + email verification ----
      'No account yet?': 'Inget konto än?',
      'Sign up': 'Registrera dig',
      'Already registered?': 'Har du redan ett konto?',
      'create account': 'skapa konto',
      'Email': 'E-post',
      'Organization name': 'Organisationsnamn',
      'Password (min 8)': 'Lösenord (minst 8)',
      'Create account': 'Skapa konto',
      'Account created — check {{email}} for a verification link.':
        'Konto skapat — kolla {{email}} efter en verifieringslänk.',
      'signup failed': 'registreringen misslyckades',
      'Verify your email': 'Verifiera din e-post',
      'We sent a link to {{email}}. Until you click it, runs and purchases are disabled.':
        'Vi skickade en länk till {{email}}. Tills du klickar på den är körningar och köp inaktiverade.',
      'Resend email': 'Skicka igen',
      'Verification email re-sent.': 'Verifieringsmejl skickat igen.',
      'Could not resend; try again later.': 'Kunde inte skicka igen; försök senare.',
      'Email verified — you can now schedule runs and buy assignments.':
        'E-post verifierad — du kan nu schemalägga körningar och köpa tilldelningar.',
      'Verification link is invalid or has expired. Sign in and tap "Resend email".':
        'Verifieringslänken är ogiltig eller har gått ut. Logga in och tryck på "Skicka igen".',
      // ---- forgot password ----
      'Forgot password?': 'Glömt lösenord?',
      'reset password': 'återställ lösenord',
      'Enter your email and we\'ll send you a link to set a new password. The link expires in 1 hour.':
        'Skriv in din e-post så skickar vi en länk för att välja ett nytt lösenord. Länken gäller i en timme.',
      'Send reset link': 'Skicka länk',
      'If {{email}} matches an account, a reset link is on its way.':
        'Om {{email}} matchar ett konto är en länk på väg.',
      'reset failed': 'återställning misslyckades',
      'Back to log in': 'Tillbaka till inloggning',
      'No reset token in the URL. Check the link you clicked, or request a new one.':
        'Ingen återställningstoken i URL:en. Kontrollera länken du klickade, eller begär en ny.',
      'Request a new link': 'Begär en ny länk',
      'New password (min 8)': 'Nytt lösenord (minst 8)',
      'Set password': 'Spara lösenord',
      'Password updated. You\'re signed in.': 'Lösenord uppdaterat. Du är inloggad.',
      // ---- account settings ----
      'Settings': 'Inställningar',
      'Manage your account. Changes are immediate; deleting your account also removes its data.':
        'Hantera ditt konto. Ändringar sker direkt; om du raderar kontot tas även dess data bort.',
      'Change password': 'Byt lösenord',
      'Current password': 'Nuvarande lösenord',
      'Update password': 'Uppdatera lösenord',
      'Saving…': 'Sparar…',
      'Password updated. Other sessions were signed out.':
        'Lösenord uppdaterat. Andra sessioner har loggats ut.',
      'Change email': 'Byt e-post',
      'Current email: {{email}}': 'Nuvarande e-post: {{email}}',
      '(none — admin-seeded account)': '(ingen — admin-skapat konto)',
      'We send a confirmation link to the new address — your email only changes when you click it.':
        'Vi skickar en bekräftelselänk till den nya adressen — e-posten ändras först när du klickar.',
      'New email': 'Ny e-post',
      'Send confirmation': 'Skicka bekräftelse',
      'Sending…': 'Skickar…',
      'Confirmation link sent to {{email}}.': 'Bekräftelselänk skickad till {{email}}.',
      'Email updated to {{email}}.': 'E-post uppdaterad till {{email}}.',
      'Could not change email.': 'Kunde inte byta e-post.',
      'Delete account': 'Radera konto',
      'This wipes your account and (if you\'re the last member) your organization, including its runs, teams, adventures and purchases. Cannot be undone.':
        'Detta raderar ditt konto och (om du är sista medlemmen) din organisation, inklusive körningar, lag, äventyr och köp. Kan inte ångras.',
      'Delete account…': 'Radera konto…',
      'Delete account?': 'Radera konto?',
      'Type your password to confirm. This is immediate and unrecoverable.':
        'Skriv ditt lösenord för att bekräfta. Detta sker direkt och kan inte ångras.',
      'Deleting…': 'Raderar…',
      // ---- data export ----
      'Download my data': 'Ladda ner min data',
      'A single JSON file with your account data plus your organization\'s runs, teams, adventures, assignments and purchases.':
        'En JSON-fil med din kontodata samt din organisations körningar, lag, äventyr, tilldelningar och köp.',
      'Preparing export…': 'Förbereder export…',
      'Downloaded {{name}}.': 'Hämtade {{name}}.',
      // ---- player forget ----
      'More': 'Mer',
      'Sign out': 'Logga ut',
      'Forget me on this run': 'Glöm mig från denna körning',
      'Forget me?': 'Glöm mig?',
      'This wipes your location history and your answers from this run. Other team members keep playing. You\'ll be signed out and the link won\'t work again. Continue?':
        'Detta raderar din platshistorik och dina svar från denna körning. Övriga lagmedlemmar fortsätter spela. Du loggas ut och länken fungerar inte igen. Fortsätt?',
      'Forget me': 'Glöm mig',
      'Cancel': 'Avbryt',
      // ---- legal ----
      'Privacy': 'Integritet',
      'Terms': 'Villkor',
      // ---- stripe receipts + refunds ----
      'Receipt': 'Kvitto',
      'Refund': 'Återbetala',
      'Refund {{amount}} to the customer? The state flips to "refunded" once Stripe confirms.':
        'Återbetala {{amount}} till kunden? Statusen ändras till "refunded" när Stripe bekräftar.',
      'Refund initiated. Webhook will flip the state shortly.':
        'Återbetalning startad. Webhook uppdaterar statusen strax.',
      'Browse the catalog and buy team slots to start scheduling runs.':
        'Bläddra i katalogen och köp lagsplatser för att börja schemalägga körningar.',
      'Open the store': 'Öppna butiken',
      // ---- age gate ----
      'I confirm no participants under 13 will play this adventure, or I have parental consent for any who will.':
        'Jag bekräftar att inga deltagare under 13 år kommer att spela detta äventyr, eller att jag har föräldrasamtycke för dem som gör det.',
      'Please confirm the player-age statement first.': 'Bekräfta åldersuttalandet först.',
      'Player ages': 'Deltagarnas ålder',
      // ---- onboarding ----
      'Welcome to Zonventure': 'Välkommen till Zonventure',
      'Three steps from here to a live adventure:': 'Tre steg till ett äventyr:',
      'Browse adventures': 'Bläddra äventyr',
      'the catalog has ready-made adventures, or you can author your own.':
        'katalogen har färdiga äventyr, eller skapa egna.',
      'Buy team slots': 'Köp lagsplatser',
      'one slot per team. Pay once per event.': 'en plats per lag. Betala en gång per event.',
      'Schedule a run': 'Schemalägg en körning',
      'add teams, share magic links, and start when ready.':
        'lägg till lag, dela magiska länkar och starta när du är klar.',
      'You can revisit this from the Settings page anytime.':
        'Du kan se detta igen från Inställningar.',
      'Skip': 'Hoppa över',
      // ---- empty states ----
      'Check back soon — operators publish new adventures regularly.':
        'Kom tillbaka snart — operatörer publicerar nya äventyr regelbundet.',
      'Pick an adventure to schedule your first run — you can add teams and share player links once it\'s created.':
        'Välj ett äventyr för att schemalägga din första körning — du kan lägga till lag och dela spelarlänkar när den är skapad.',
      'Use "Add puzzle" above to author a new one or copy from the library.':
        'Använd "Lägg till pussel" ovan för att skapa eller kopiera från biblioteket.',
      // ---- catch-up batch (caught by the i18n completeness test) ----
      '(unnamed)': '(namnlös)',
      'Branding': 'Varumärke',
      'Click to write…': 'Klicka för att skriva…',
      'Clue in markdown… (click to edit)': 'Ledtråd i markdown… (klicka för att redigera)',
      'Copied {{n}} captain links': 'Kopierade {{n}} kaptenslänkar',
      'Copied {{n}} rows': 'Kopierade {{n}} rader',
      'Copy all (CSV)': 'Kopiera alla (CSV)',
      'Copy captain link': 'Kopiera kaptenslänk',
      'Copy captain links': 'Kopiera kaptenslänkar',
      'Copy from library…': 'Kopiera från biblioteket…',
      'Details': 'Detaljer',
      'Distribute links': 'Dela länkar',
      'Filter {{n}} teams…': 'Filtrera {{n}} lag…',
      'Fullscreen (f)': 'Helskärm (f)',
      'GPS ±{{acc}} m — too imprecise': 'GPS ±{{acc}} m — för oprecist',
      'Intro — shown on the welcome screen': 'Intro — visas på välkomstskärmen',
      'Markdown supported: **bold**, *italics*, [links](https://…), `code`, - bullet lists.':
        'Markdown stöds: **fet**, *kursiv*, [länkar](https://…), `kod`, - punktlistor.',
      'More options': 'Fler alternativ',
      'Outro — shown on the completion screen': 'Outro — visas när äventyret är klart',
      'Preview appears here as you type.': 'Förhandsvisning visas här medan du skriver.',
      'Show 1 player link': 'Visa 1 spelarlänk',
      'Show {{n}} player links': 'Visa {{n}} spelarlänkar',
      'Shown on the first-run welcome overlay — leave empty for the default welcome + tips.':
        'Visas i välkomstöverlägget vid första körningen — lämna tomt för standardvälkomsten.',
      'Shown under the stats when the team finishes — leave empty for the default celebratory line.':
        'Visas under statistiken när laget är klart — lämna tomt för standardraden.',
      'Start manually — I\'ll hit Start when we\'re ready': 'Starta manuellt — jag trycker Start när vi är redo',
      'Toggle fullscreen': 'Växla helskärm',
      'Waiting for GPS lock…': 'Väntar på GPS-låsning…',
      'What the player sees… (click to edit, markdown supported)':
        'Det här ser spelaren… (klicka för att redigera, markdown stöds)',
      'geolocation not supported on this device': 'positionsbestämning stöds inte på denna enhet',
      'no signal — try moving outdoors': 'ingen signal — försök gå utomhus',
      'permission denied — enable location for this site':
        'åtkomst nekad — aktivera plats för denna sida',
      'puzzle': 'pussel',
      'puzzles': 'pussel',
      'timed out — retrying': 'tidsgräns nådd — försöker igen',
      '{{n}} teams — captain + per-player links': '{{n}} lag — kapten + spelarlänkar',
      // ---- live countdown + start animation ----
      'Starting soon': 'Börjar snart',
      'until the flag drops': 'tills starten',
      'waiting for the arranger': 'väntar på arrangören',
      'Scheduled for {{when}}': 'Planerad till {{when}}',
      'any moment…': 'när som helst…',
      'Go!': 'Kör igång!',
      "We're live!": 'Nu kör vi!',
      'Teams are on the move': 'Lagen är igång',
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
