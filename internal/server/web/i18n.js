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
      'Events': 'Event',
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
      'Drag to reorder': 'Dra för att ordna om',
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
      'verify': 'verifiera',
      'Manually verify email (use only after confirming the user out-of-band)':
        'Verifiera e-post manuellt (använd bara efter att du bekräftat användaren utanför systemet)',
      'Manually verify {{username}}\'s email? Only do this after confirming the user out-of-band — this bypasses the verification mail loop.':
        'Verifiera {{username}}s e-post manuellt? Gör bara detta efter att du bekräftat användaren utanför systemet — detta hoppar över verifieringsmejlet.',
      'Email verified': 'E-post verifierad',
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
      'Pick one. The organisation can schedule events of this adventure without buying team slots.':
        'Välj en. Organisationen kan schemalägga event av detta äventyr utan att köpa lagplatser.',
      'Revoke share': 'Återkalla delning',
      'Every organisation already has access.': 'Alla organisationer har redan åtkomst.',
      'Revoke share with {{org}}? Existing events keep working; new events will be blocked.':
        'Återkalla delning med {{org}}? Pågående event fortsätter, men nya event blockeras.',
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
      'Scan QR code': 'Skanna QR-kod',
      'Point the camera at a QR code': 'Rikta kameran mot en QR-kod',
      'Camera permission denied.': 'Kameran nekades åtkomst.',
      'Camera unavailable.': 'Kameran är inte tillgänglig.',
      'Test scan': 'Testskanna',
      'Test scan: opens the camera and shows what your QR encodes':
        'Testskanna: öppnar kameran och visar vad din QR-kod innehåller',
      'QR contains "{{v}}" — accepted.': 'QR innehåller ”{{v}}” — godkänt.',
      'QR contains "{{v}}" — not in accept list.':
        'QR innehåller ”{{v}}” — finns inte i listan över godkända svar.',
      'Try an answer (preview)': 'Pröva ett svar (förhandsvisning)',
      'Waiting for the adventure to start…': 'Väntar på att äventyret ska börja…',
      'The arranger paused the event. Stand by.':
        'Arrangören har pausat eventet. Vänta.',
      'Travel to the location': 'Bege dig till platsen',
      'Head within {{r}} m of ({{lat}}, {{lng}}).':
        'Gå inom {{r}} m från ({{lat}}, {{lng}}).',
      'Last fix: {{lat}}, {{lng}} (±{{acc}} m)':
        'Senaste position: {{lat}}, {{lng}} (±{{acc}} m)',
      'Waiting for a GPS fix…': 'Väntar på GPS-position…',
      'Send location now': 'Skicka position nu',
      'Hint: {{text}}': 'Ledtråd: {{text}}',
      // ---- hint system ----
      'Hints': 'Ledtrådar',
      'Hint': 'Ledtråd',
      'Hint {{i}}/{{n}}': 'Ledtråd {{i}}/{{n}}',
      'Tap below to reveal your first hint. Use them sparingly — you can always come back.':
        'Tryck nedan för att se din första ledtråd. Använd dem sparsamt — du kan alltid komma tillbaka.',
      'Reveal hint': 'Visa ledtråd',
      'Reveal next hint': 'Visa nästa ledtråd',
      "The next hint isn't ready yet. Keep trying for a little longer.":
        'Nästa ledtråd är inte redo ännu. Kämpa på ett tag till.',
      // ---- hint editor (arrange) ----
      'Add hint': 'Lägg till ledtråd',
      'Remove hint': 'Ta bort ledtråd',
      'Clue in markdown…': 'Ledtråd i markdown…',
      'No hints yet — add one below for a delayed helper button.':
        'Inga ledtrådar ännu — lägg till en nedan så dyker en hjälpknapp upp efter en stund.',
      'Unlock after': 'Lås upp efter',
      's': 's',
      '(immediate)': '(direkt)',
      'Each hint unlocks a configurable number of seconds after the team reaches this puzzle. Players reveal them one at a time with a button.':
        'Varje ledtråd låses upp ett konfigurerbart antal sekunder efter att laget når detta pussel. Spelare visar dem en i taget med en knapp.',
      'Disconnected — try again in a moment.':
        'Frånkopplad — försök igen om en stund.',
      'Reconnecting…': 'Återansluter…',
      'Disconnected — check your connection.':
        'Frånkopplad — kontrollera din anslutning.',
      'Too many attempts. Try again in {{n}}s.':
        'För många försök. Prova igen om {{n}} s.',
      'Try again now.': 'Prova igen nu.',
      "You're not at the location yet — move closer and try again.":
        'Du är inte framme än — flytta dig närmare och försök igen.',
      'Your link expired': 'Din länk har gått ut',
      "Magic links expire after a while. Ask your arranger for a fresh link to get back in — your team's progress is safe.":
        'Magiska länkar går ut efter en tid. Be din arrangör om en ny länk för att komma tillbaka — lagets framsteg finns kvar.',
      'Event cancelled': 'Eventet avbröts',
      'The arranger cancelled this event. Reach out to them for next steps; nothing more to do here.':
        'Arrangören avbröt eventet. Hör av dig till dem för nästa steg; det finns inget mer att göra här.',
      'Already open in another tab': 'Redan öppen i en annan flik',
      "This game is already running in another tab on this device. Close this one and continue there to keep your answers and location in sync.":
        'Spelet körs redan i en annan flik på den här enheten. Stäng den här och fortsätt där så att svar och position hålls synkade.',
      'Use this tab instead': 'Använd den här fliken istället',
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
      'Loading your event stats…': 'Läser in din statistik…',
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
      'Event': 'Event',
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
      'Events total': 'Totalt event',
      'Live now': 'Pågår nu',
      'Scheduled': 'Schemalagda',
      'Recent events': 'Senaste eventen',
      'Getting started': 'Kom igång',
      'A few clicks to your first adventure.': 'Några klick till ditt första äventyr.',
      'Pick an adventure — browse the Store or create your own':
        'Välj ett äventyr — bläddra i butiken eller skapa ett eget',
      'Have team slots ready — an assignment lets you schedule an event':
        'Ha lagplatser redo — en tilldelning låter dig schemalägga en event',
      'Schedule an event, add teams, share their links — and start!':
        'Schemalägg en event, lägg till lag, dela länkarna — och kör!',
      'Schedule': 'Schemalägg',
      'Go': 'Kör',
      'Start a new event': 'Starta ett nytt event',
      'Unused assignments': 'Oanvända tilldelningar',
      'Signed in as admin — you manage organizations, their assignments, and other admins.':
        'Inloggad som administratör — du hanterar organisationer, deras tilldelningar och övriga administratörer.',
      'Org users': 'Organisationsanvändare',
      'Active events': 'Aktiva event',
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
      '{{section}} / Event #{{id}}': '{{section}} / Event #{{id}}',
      '{{section}} / {{slug}}': '{{section}} / {{slug}}',
      // ---- adventures list (arranger) ----
      'No adventures you can use yet.': 'Inga äventyr du kan köra än.',
      'Create your own or browse the Store for published adventures.':
        'Skapa ett eget eller bläddra i butiken efter publicerade äventyr.',
      'Browse the Store': 'Bläddra i butiken',
      'Yours': 'Eget',
      '{{n}} puzzle': '{{n}} pussel',
      '{{n}} assignment available': '{{n}} tilldelning tillgänglig',
      '{{n}} assignments available': '{{n}} tilldelningar tillgängliga',
      '{{n}} event held': '{{n}} event bokad',
      '{{n}} events held': '{{n}} event bokade',
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
      'Schedule event': 'Schemalägg event',
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
        'Eventet skapas i schemalagt läge. Du kan starta den manuellt senare, eller välja en lokal tid här så startar servern den automatiskt.',
      'Auto-start at (optional)': 'Autostart kl. (valfritt)',
      'Create event': 'Skapa event',
      'Event #{{id}} scheduled': 'Event #{{id}} schemalagd',
      // ---- runs table ----
      'No events yet.': 'Inga event än.',
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
      'Event #{{id}}': 'Event #{{id}}',
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
      'Live for {{t}}': 'Kör sedan {{t}}',
      'Open live view': 'Öppna livevy',
      'Fake a race so you can see the live view animate without recruiting real players. Writes to the same tables as a real event, so every subscriber (map, leaderboard, WS) updates as if players were on the ground.':
        'Fejka en tävling så att du kan se livevyn animera utan att värva riktiga spelare. Skriver till samma tabeller som en riktig event, så alla prenumeranter (karta, topplista, WS) uppdateras som om spelarna var på plats.',
      'Configuration': 'Inställningar',
      'Teams (2–20)': 'Lag (2–20)',
      'Speed multiplier (1–50)': 'Hastighetsmultiplikator (1–50)',
      'Higher speed advances the walk faster without changing the ping cadence, so fast-forwarded events still look natural.':
        'Högre hastighet snabbar på vandringen utan att ändra ping-frekvensen, så snabbspolade event ser naturliga ut.',
      '(no adventures)': '(inga äventyr)',
      'Simulation started': 'Simulering startad',
      'Simulation reset': 'Simulering återställd',
      'Network error — is the server running?': 'Nätverksfel — kör servern?',
      'Start this event now?': 'Starta eventet nu?',
      'Starting the event opens the first puzzle to all teams. ':
        'När eventet startas öppnas det första pusslet för alla lag. ',
      'An auto-start is currently scheduled for ':
        'En autostart är schemalagd till ',
      '. Starting now will replace that.': '. Om du startar nu ersätts den.',
      'You can pause it later if needed.': 'Du kan pausa den senare om det behövs.',
      'Start event': 'Starta event',
      'Change auto-start': 'Ändra autostart',
      'Set auto-start': 'Ställ in autostart',
      'The server will start the event automatically at the time below. Leave empty and hit Clear to remove the auto-start.':
        'Servern sätter eventet till pågår automatiskt vid tiden nedan. Lämna tomt och tryck Rensa för att ta bort autostarten.',
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
      'Each organization is a tenant — its users (org_admin, org_member) operate the games. Assign adventures to an organization to let them schedule events.':
        'Varje organisation är en tenant — dess användare (org_admin, org_member) driver spelen. Tilldela äventyr till en organisation så att de kan schemalägga event.',
      '{{n}} user': '{{n}} användare',
      '{{n}} users': '{{n}} användare',
      '{{n}} player': '{{n}} spelare',
      '{{n}} players': '{{n}} spelare',
      '{{n}} event': '{{n}} event',
      '{{n}} events': '{{n}} event',
      'Add user': 'Lägg till användare',
      'Users ({{n}})': 'Användare ({{n}})',
      'No users yet — add one to get started.': 'Inga användare än — lägg till en för att komma igång.',
      '{{role}} · click ⓧ to remove': '{{role}} · klicka på ⓧ för att ta bort',
      '{{username}} (admin)': '{{username}} (administratör)',
      '{{username}} (member)': '{{username}} (medlem)',
      'Unused assignments ({{n}})': 'Oanvända tilldelningar ({{n}})',
      'None — assign an adventure to let them schedule an event.':
        'Inga — tilldela ett äventyr så att de kan schemalägga en event.',
      'Click to edit': 'Klicka för att redigera',
      '{{slug}} · {{n}} team': '{{slug}} · {{n}} lag',
      '{{slug}} · {{n}} teams': '{{slug}} · {{n}} lag',
      'Used ({{n}})': 'Använda ({{n}})',
      'Click to edit team cap': 'Klicka för att ändra lagtak',
      '{{slug}} · event #{{event}} · max {{max}}': '{{slug}} · event #{{run}} · max {{max}}',
      'New organization': 'Ny organisation',
      'Name': 'Namn',
      'Organization created': 'Organisation skapad',
      'Rename {{name}}': 'Byt namn på {{name}}',
      'New name': 'Nytt namn',
      'Delete organization “{{name}}”?': 'Ta bort organisationen ”{{name}}”?',
      'This deletes every user, player, assignment and event that belongs to the organization. Enter your password to confirm.':
        'Det raderar alla användare, spelare, tilldelningar och event som tillhör organisationen. Ange ditt lösenord för att bekräfta.',
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
      'Linked to event #{{id}}. You may raise the cap; lowering below the current team count is rejected.':
        'Kopplad till event #{{id}}. Du kan höja taket; lägre än nuvarande antal lag avvisas.',
      'This assignment has not yet been used.': 'Tilldelningen har inte använts än.',
      'Max teams must be at least 1': 'Max antal lag måste vara minst 1',
      'Assignment updated': 'Tilldelning uppdaterad',
      'No adventures loaded': 'Inga äventyr inlästa',
      'Assign adventure to {{name}}': 'Tilldela äventyr till {{name}}',
      'Each assignment may be used for exactly one event. For more, assign again.':
        'Varje tilldelning kan användas för exakt en event. Tilldela igen för fler.',
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
      'Hint:': 'Ledtråd:',
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
      'Event created for ': 'Event skapad för ',
      'Event started': 'Eventet startade',
      'Event paused': 'Eventet pausades',
      'Event resumed': 'Eventet återupptogs',
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
      'New event': 'Ny event',
      'Rename event': 'Byt namn på event',
      'Event name': 'Eventets namn',
      'Event name (optional)': 'Eventets namn (valfritt)',
      'Event ready': 'Eventet är klar',
      'Event creation failed': 'Kunde inte skapa event',
      'Empty resets to "Event #{{id}}".': 'Tomt återställer till "Event #{{id}}".',
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
      'Payment received — let\'s schedule your event.':
        'Betalning mottagen — dags att schemalägga din event.',
      'Proceed to payment': 'Fortsätt till betalning',
      'How many team slots do you need? ($2.00 each, simulated payment.)':
        'Hur många lagplatser behöver du? ($2,00 styck, simulerad betalning.)',
      'How many team slots do you need? ({{price}} each, simulated payment.)':
        'Hur många lagplatser behöver du? ({{price}} styck, simulerad betalning.)',
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
      'I agree to the ': 'Jag godkänner ',
      'Terms of Service': 'användarvillkoren',
      ' and ': ' och ',
      'Privacy Policy': 'integritetspolicyn',
      'Account created — check {{email}} for a verification link.':
        'Konto skapat — kolla {{email}} efter en verifieringslänk.',
      'signup failed': 'registreringen misslyckades',
      'Verify your email': 'Verifiera din e-post',
      'We sent a link to {{email}}. Until you click it, events and purchases are disabled.':
        'Vi skickade en länk till {{email}}. Tills du klickar på den är körningar och köp inaktiverade.',
      'Resend email': 'Skicka igen',
      'Verification email re-sent.': 'Verifieringsmejl skickat igen.',
      'Times are interpreted as your local timezone.':
        'Tiderna tolkas i din lokala tidszon.',
      'Times are interpreted as your local timezone ({{tz}}).':
        'Tiderna tolkas i din lokala tidszon ({{tz}}).',
      'Run events, manage teams, plus invite/remove users and change billing.':
        'Kör event, hantera lag, samt bjuda in/ta bort användare och ändra fakturering.',
      'Run events and manage teams. Cannot invite users or touch billing.':
        'Kör event och hantera lag. Kan inte bjuda in användare eller röra fakturering.',
      'Links expire after a fixed window — anyone with the URL can play as that player. Use the per-team rotate-token action if a link leaks; existing players keep playing until they sign out.':
        'Länkar går ut efter en bestämd tid — vem som helst med URL:en kan spela som den spelaren. Använd "Byt token" per lag om en länk läcker; befintliga spelare fortsätter tills de loggar ut.',
      "Password updated. You're still signed in here; every other session was kicked.":
        'Lösenordet är uppdaterat. Du är fortfarande inloggad här; alla andra sessioner blev utloggade.',
      'Open server logs around this time':
        'Öppna serverloggar kring denna tid',
      'Could not resend; try again later.': 'Kunde inte skicka igen; försök senare.',
      'Email verified — you can now schedule events and buy assignments.':
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
      'Forget me on this event': 'Glöm mig från denna körning',
      'Erase my play data': 'Radera mina speldata',
      'Theme': 'Tema',
      'Light': 'Ljust',
      'Dark': 'Mörkt',
      'Auto': 'Auto',
      'Team': 'Lag',
      'Show team join link': 'Visa lagets anslutningslänk',
      'A QR code for this team\'s join page. A teammate without a magic link can scan it, pick their name, and start playing.':
        'En QR-kod till lagets anslutningssida. En lagkamrat utan magisk länk kan skanna, välja sitt namn och börja spela.',
      'Team join link': 'Lagets anslutningslänk',
      'Scan to join this team. Each teammate picks their own name on the page.':
        'Skanna för att gå med i laget. Varje lagkamrat väljer sitt eget namn på sidan.',
      'Team join QR code': 'QR-kod för att gå med i laget',
      'Copied': 'Kopierad',
      'Permanently deletes your location pings and answer attempts from this event and revokes your link. Use at end of event or for privacy reasons.':
        'Raderar permanent dina positioner och svarsförsök från denna körning och spärrar din länk. Använd efter eventet eller av integritetsskäl.',
      'Menu': 'Meny',
      'Account': 'Konto',
      'Forget me?': 'Glöm mig?',
      'This wipes your location history and your answers from this event. Other team members keep playing. You\'ll be signed out and the link won\'t work again. Continue?':
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
      'Browse the catalog and buy team slots to start scheduling events.':
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
      'Schedule an event': 'Schemalägg en körning',
      'add teams, share magic links, and start when ready.':
        'lägg till lag, dela magiska länkar och starta när du är klar.',
      'You can revisit this from the Settings page anytime.':
        'Du kan se detta igen från Inställningar.',
      'Skip': 'Hoppa över',
      // ---- empty states ----
      'Check back soon — operators publish new adventures regularly.':
        'Kom tillbaka snart — operatörer publicerar nya äventyr regelbundet.',
      'Pick an adventure to schedule your first event — you can add teams and share player links once it\'s created.':
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
      'Shown on the first-event welcome overlay — leave empty for the default welcome + tips.':
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
      // ---- webhooks ----
      'Webhooks': 'Webhooks',
      'Add endpoint': 'Lägg till slutpunkt',
      'Add webhook': 'Lägg till webhook',
      'Edit webhook': 'Redigera webhook',
      'Endpoint URL': 'URL för slutpunkt',
      'Subscribed events': 'Prenumererade händelser',
      'Send test': 'Skicka test',
      'Send test event': 'Skicka testhändelse',
      'Rotate secret': 'Rotera nyckel',
      'Rotate secret?': 'Rotera nyckel?',
      'Disable': 'Inaktivera',
      'Enable': 'Aktivera',
      'Disabled': 'Inaktiverad',
      'Delete endpoint': 'Ta bort slutpunkt',
      'Delete endpoint?': 'Ta bort slutpunkten?',
      'Endpoint deleted.': 'Slutpunkten borttagen.',
      'Endpoint disabled.': 'Slutpunkten inaktiverad.',
      'Endpoint enabled.': 'Slutpunkten aktiverad.',
      'Webhook created': 'Webhook skapad',
      'Webhook updated.': 'Webhook uppdaterad.',
      'New webhook secret': 'Ny webhook-nyckel',
      'Secret copied': 'Nyckel kopierad',
      'Copy failed': 'Kopiering misslyckades',
      "I've saved it": 'Jag har sparat den',
      'Secret hint: ': 'Nyckelledtråd: ',
      'Created {{date}}': 'Skapad {{date}}',
      'Test queued. Watch your endpoint logs (delivery within ~5s).':
          'Test köat. Bevaka loggarna på din slutpunkt (levereras inom ~5 s).',
      'Re-enable the endpoint first.': 'Aktivera slutpunkten först.',
      'Select at least one event kind.': 'Välj minst en händelsetyp.',
      'Could not load webhooks ({{status}}).': 'Kunde inte ladda webhooks ({{status}}).',
      'No webhook endpoints yet. Add one to receive lifecycle events on a URL you control.':
          'Inga webhook-slutpunkter än. Lägg till en för att ta emot livscykelhändelser på en URL du kontrollerar.',
      'Outbound HTTP callbacks for run / team / purchase lifecycle. Each delivery is signed with HMAC-SHA256; verify the X-Zv-Signature header against your endpoint\'s secret.':
          'Utgående HTTP-anrop för event, lag och köphändelser. Varje leverans signeras med HMAC-SHA256; verifiera X-Zv-Signature mot slutpunktens nyckel.',
      'Must be https. Localhost is allowed for development.':
          'Måste vara https. Localhost tillåts vid utveckling.',
      'Mint a new secret. The old one stops working immediately.':
          'Skapa en ny nyckel. Den gamla slutar fungera direkt.',
      'This invalidates the current secret immediately. Deploy the new secret to your verifier first if you want zero downtime.':
          'Detta gör nuvarande nyckel ogiltig direkt. Distribuera den nya nyckeln till din verifierare först om du vill undvika avbrott.',
      'This is the only time the secret will be shown. Store it somewhere safe.':
          'Detta är enda gången nyckeln visas. Förvara den säkert.',
      'This stops all future deliveries to this URL. Past deliveries are removed too. Cannot be undone.':
          'Detta stoppar alla framtida leveranser till denna URL. Tidigare leveranser tas också bort. Går inte att ångra.',
      'Configure your verifier with this value. Compute HMAC-SHA256 over the raw request body and compare to the X-Zv-Signature header (after stripping the "sha256=" prefix).':
          'Konfigurera din verifierare med detta värde. Beräkna HMAC-SHA256 över rå begärankropp och jämför med X-Zv-Signature-huvudet (efter att "sha256="-prefixet tagits bort).',
      '(no events subscribed)': '(inga händelser prenumereras)',
      '(unnamed endpoint)': '(slutpunkt utan namn)',
      'e.g. prod-slack': 't.ex. prod-slack',
      // ---- webhook delivery log ----
      'Delivery log': 'Leveranslogg',
      'Webhook delivery log': 'Webhook-leveranslogg',
      'Delivery log: {{name}}': 'Leveranslogg: {{name}}',
      'Inspect this endpoint\'s delivery history':
          'Inspektera den här slutpunktens leveranshistorik',
      'Click a row to see the raw JSON we POSTed and the verifier-side signature inputs.':
          'Klicka på en rad för att se den råa JSON vi skickade och signaturindata för verifieraren.',
      'Back to webhooks': 'Tillbaka till webhooks',
      'Every attempt to deliver an event to one of your endpoints. Click a row to see the raw JSON we POSTed and the verifier-side signature inputs.':
          'Varje försök att leverera en händelse till en av dina slutpunkter. Klicka på en rad för att se den råa JSON vi skickade och signaturindata för verifieraren.',
      'All endpoints': 'Alla slutpunkter',
      'Any status': 'Alla statusar',
      'Pending': 'Väntar',
      'Waiting': 'Väntar',
      'Delivered': 'Levererad',
      'Dead': 'Misslyckad',
      'No response': 'Inget svar',
      // ---- adventures tabs + settings cards ----
      'Yours': 'Dina',
      'Catalog': 'Katalog',
      'Browse the Catalog': 'Bläddra i katalogen',
      'Create your own or browse the Catalog for published adventures.':
          'Skapa ditt eget eller bläddra i katalogen för publicerade äventyr.',
      'Integrations': 'Integrationer',
      'Configure webhooks': 'Konfigurera webhooks',
      'Send your event lifecycle into Slack, Zapier, or your own systems via signed HTTP callbacks.':
          'Skicka din eventlivscykel till Slack, Zapier eller egna system via signerade HTTP-anrop.',
      'Help': 'Hjälp',
      'Open the manual': 'Öppna manualen',
      'Step-by-step walk-through of authoring adventures, scheduling events, adding teams, and integrating.':
          'Steg-för-steg-guide för att skapa äventyr, schemalägga event, lägga till lag och integrera.',
      'Refresh': 'Uppdatera',
      'Could not load deliveries ({{status}}).': 'Kunde inte ladda leveranser ({{status}}).',
      'No deliveries yet. Send a test from the Webhooks page to see one land here.':
          'Inga leveranser än. Skicka ett test från Webhooks-sidan för att se en dyka upp här.',
      'Showing {{n}} delivery row(s).': 'Visar {{n}} leveransrad(er).',
      'Attempts: ': 'Försök: ',
      'Delivered at: ': 'Levererad: ',
      'Next attempt: ': 'Nästa försök: ',
      // ---- manual ----
      // Nav label intentionally kept as "Manual" in Swedish — the
      // word is widely understood and matches the operator's brand
      // preference. Translating to "Handbok" would otherwise also
      // be valid.
      'Manual': 'Manual',
      'Arranger manual': 'Manual för arrangörer',
      'A pedagogic walk-through of the platform: create an adventure, fill it with puzzles, schedule an event, add teams, and watch them play. Read it once start to finish, or jump to a section from the table of contents.':
          'En pedagogisk genevent av plattformen: skapa ett äventyr, fyll det med pussel, schemalägg en event, lägg till lag och se dem spela. Läs igenom från början till slut, eller hoppa till ett avsnitt via innehållsförteckningen.',
      'Contents': 'Innehåll',
      // section titles
      'Welcome': 'Välkommen',
      'First steps': 'Första stegen',
      'Authoring an adventure': 'Skapa ett äventyr',
      'Adding puzzles': 'Lägg till pussel',
      'How the event flow fits together': 'Så hänger flödet ihop',
      'Buying team-slot assignments': 'Köp lagplatser (uppdrag)',
      'Creating an event': 'Skapa en event',
      'Adding teams and sharing magic links': 'Lägg till lag och dela magiska länkar',
      'Running the event': 'Genomför eventet',
      'Webhooks: outbound integrations': 'Webhooks: utgående integrationer',
      'Account, data and deletion': 'Konto, data och radering',
      'Getting help': 'Få hjälp',
      // welcome
      'Zonventure is a platform for running location-based adventure games — treasure hunts, team-building events, school field trips. As an arranger you author the puzzles, set up an event, and the players walk around with their phones answering them.':
          'Zonventure är en plattform för platsbaserade äventyrsspel — skattjakter, lagevent, skolutflykter. Som arrangör skapar du pusslen, sätter upp en event, och spelarna går runt med sina telefoner och svarar på dem.',
      'Four words to know': 'Fyra ord att kunna',
      'Adventure': 'Äventyr',
      'a reusable script. Pages of puzzles in order. Like a book.':
          'ett återanvändbart manus. Sidor med pussel i ordning. Som en bok.',
      'Assignment': 'Uppdrag',
      'a paid licence to use an adventure for an event once for up to N teams. You buy these from the Store.':
          'en betald licens att köra ett äventyr en gång för upp till N lag. Köps i Butiken.',
      'one specific playthrough at a specific time. Created from an assignment.':
          'en specifik genomspelning vid en specifik tidpunkt. Skapas från ett uppdrag.',
      'a group of players in an event, identified by a team name. Each player gets their own magic-link to log in as part of the team.':
          'en grupp spelare i en event, identifierad med ett lagnamn. Varje spelare får en egen magisk länk för att logga in som del av laget.',
      'Mental model: an Adventure is a recipe; an Assignment is the right to cook it once; a Event is one specific dinner; a Team is the people sitting at a table.':
          'Tankebild: ett Äventyr är ett recept; ett Uppdrag är rätten att laga det en gång; en Event är en specifik middag; ett Lag är personerna vid bordet.',
      // first steps
      'When you sign up, you create an organization. Everyone in your organization shares the same adventures, assignments, and events.':
          'När du registrerar dig skapar du en organisation. Alla i din organisation delar samma äventyr, uppdrag och event.',
      'Verify your email': 'Verifiera din e-post',
      'After signup we email a verification link. You can log in before clicking it, but money-touching actions (buying assignments, scheduling events) are blocked until you verify. The dashboard shows a banner with a "Resend" button if it didn\'t arrive.':
          'Efter registreringen skickar vi en verifieringslänk. Du kan logga in innan du klickar, men åtgärder som rör pengar (köp av uppdrag, schemaläggning av event) blockeras tills du verifierar. Översikten visar en banderoll med en "Skicka igen"-knapp om mejlet inte kom fram.',
      '[Screenshot: dashboard with the unverified-email banner]':
          '[Skärmdump: översikt med banderoll om overifierad e-post]',
      'The dashboard': 'Översikten',
      'The dashboard is your home: at-a-glance view of upcoming events, ready-to-schedule assignments, and shortcuts to the things you most often do.':
          'Översikten är ditt hem: en snabb vy över kommande event, uppdrag redo att schemaläggas och genvägar till det du gör oftast.',
      '[Screenshot: a populated arranger dashboard]':
          '[Skärmdump: en ifylld arrangörsöversikt]',
      // adventure
      'Adventures live in the Library. You can author your own, or pick one from the global catalog (the catalog is curated by the platform admin and always free to use).':
          'Äventyr finns i Biblioteket. Du kan skapa egna eller köra ett från den globala katalogen (katalogen kureras av plattformsadministratören och är alltid gratis att använda).',
      'Create an adventure': 'Skapa ett äventyr',
      'Click "Library" in the sidebar.': 'Klicka på "Bibliotek" i sidomenyn.',
      'Press "New adventure".': 'Tryck på "Nytt äventyr".',
      'Give it a title. The slug is auto-derived but editable; once players have started an event with this slug, don\'t change it.':
          'Ge det en titel. URL-namnet (slug) härleds automatiskt men går att redigera; ändra det inte när spelare har startat en event med detta slug.',
      'Save. You\'re taken to the puzzle editor.':
          'Spara. Du tas till pusselredigeraren.',
      '[Screenshot: Library tab with the New adventure button highlighted]':
          '[Skärmdump: Bibliotek-fliken med knappen Nytt äventyr markerad]',
      'Adventure metadata': 'Äventyrets metadata',
      'Beyond title and slug, an adventure can carry:':
          'Utöver titel och slug kan ett äventyr ha:',
      'Intro and outro text': 'Intro- och outrotext',
      'markdown shown to players before the first puzzle and after they finish. Set the tone here ("Welcome to the King\'s Hunt!") and wrap up with congratulations.':
          'markdown som visas för spelarna före första pusslet och efter att de är klara. Sätt tonen här ("Välkommen till Kungens jakt!") och avsluta med en gratulation.',
      'Colors': 'Färger',
      'two accent colours that override the default pink branding for players in this adventure. Useful for white-labelled events.':
          'två accentfärger som ersätter standardrosa branding för spelarna i detta äventyr. Användbart för white-label-event.',
      'Avatar': 'Avatar',
      'an image rendered as the adventure\'s logo on the dashboard, the live view, and the player welcome screen.':
          'en bild som visas som äventyrets logotyp på översikten, livevyn och spelarnas välkomstskärm.',
      // puzzles
      'A puzzle is one page of the adventure. Players see them in order; only after solving the current page do they advance to the next one.':
          'Ett pussel är en sida i äventyret. Spelarna ser dem i ordning; först efter att de löst aktuell sida går de vidare till nästa.',
      '[Screenshot: the puzzle editor showing a puzzle in edit mode]':
          '[Skärmdump: pusselredigeraren med ett pussel i redigeringsläge]',
      'Title and prompt': 'Titel och uppgiftstext',
      'The title appears as a heading on the player\'s screen. The prompt is the body — render it with markdown for formatting, links, lists.':
          'Titeln visas som rubrik på spelarens skärm. Uppgiftstexten är brödtexten — använd markdown för formatering, länkar och listor.',
      'Tip: keep prompts short. Players read them on a phone, often outdoors.':
          'Tips: håll uppgiftstexterna korta. Spelarna läser på telefon, ofta utomhus.',
      'Answer types': 'Svarstyper',
      'Text answer': 'Textsvar',
      'list one or more accepted answers. The player\'s typed answer is matched case-insensitively, with whitespace trimmed. Use multiple entries to allow synonyms ("blue", "blå", "blueberry").':
          'lista ett eller flera accepterade svar. Spelarens svar matchas utan hänsyn till versaler/gemener, mellanslag trimmas. Lägg flera poster för att tillåta synonymer ("blue", "blå", "blåbär").',
      'No answer': 'Inget svar',
      'leave the accept list empty. The player sees a "Continue" button instead of an input field. Useful for narrative pages or location-only puzzles.':
          'lämna listan med accepterade svar tom. Spelaren ser en "Fortsätt"-knapp istället för ett textfält. Bra för berättande sidor eller pussel som bara handlar om plats.',
      'Hints': 'Ledtrådar',
      'Each puzzle can carry an ordered list of hints, each with a delay (in seconds) before it becomes revealable. The "Reveal hint" button on the player\'s screen unveils them one at a time.':
          'Varje pussel kan ha en ordnad lista med ledtrådar, var och en med en fördröjning (i sekunder) innan den kan visas. Knappen "Visa ledtråd" på spelarens skärm avslöjar dem en i taget.',
      'Delays are measured from when the team arrives at the puzzle, so a stuck team naturally gets help, while a fast team never sees the hints.':
          'Fördröjningarna mäts från när laget anländer till pusslet, så ett fastlåst lag får naturligt hjälp medan ett snabbt lag aldrig ser ledtrådarna.',
      'Images': 'Bilder',
      'Attach up to a few images per puzzle. They render under the prompt. Photos of the location, illustrations, or visual clues. Each image is capped at 5 MB.':
          'Bifoga några bilder per pussel. De visas under uppgiftstexten. Foton av platsen, illustrationer eller visuella ledtrådar. Varje bild får vara max 5 MB.',
      'Location gates': 'Platsgrindar',
      'Optionally pin the puzzle to a GPS location with a radius. Players can read the prompt anywhere, but they can\'t submit the answer until they\'re inside the radius. The map widget in the editor lets you click to place the pin.':
          'Du kan valfritt knyta pusslet till en GPS-plats med en radie. Spelarna kan läsa uppgiftstexten var som helst men kan inte skicka svar förrän de är inom radien. Kartwidgeten i redigeraren låter dig klicka för att placera nålen.',
      '[Screenshot: the location-gate map widget]':
          '[Skärmdump: kartwidgeten för platsgrind]',
      'GPS accuracy varies. A 50-metre radius is usually a sensible minimum — under that, urban canyons and indoor walls cause false negatives.':
          'GPS-noggrannheten varierar. 50 meter är oftast ett rimligt minimum — under det orsakar gränder mellan höghus och inomhusväggar felaktiga negativ.',
      'Reordering and disabling': 'Omordna och inaktivera',
      'Drag puzzles to reorder. Disable a puzzle to hide it from players without deleting it — useful for swapping seasonal content.':
          'Dra pusslen för att ändra ordning. Inaktivera ett pussel för att dölja det för spelarna utan att radera det — bra för säsongsväxlingar.',
      'Preview': 'Förhandsgranska',
      'The "Preview" button on the adventure detail page renders a sandbox playthrough. No team progress is recorded — it\'s your test rig.':
          'Knappen "Förhandsgranska" på äventyrets detaljsida öppnar en sandlåda för genomspelning. Ingen lagprogress sparas — det är din testbänk.',
      // run flow
      'Before scheduling your first event, take a moment to understand the chain:':
          'Innan du schemalägger din första event, ta en stund att förstå kedjan:',
      'Adventure  →  Assignment  →  Event  →  Teams  →  Players':
          'Äventyr  →  Uppdrag  →  Event  →  Lag  →  Spelare',
      'You author or pick an Adventure (free).':
          'Du skapar eller väljer ett Äventyr (gratis).',
      'You buy an Assignment for that adventure: pays for up to N teams.':
          'Du köper ett Uppdrag för det äventyret: betalar för upp till N lag.',
      'You create a Event from the Assignment: ties it to a specific date/time.':
          'Du skapar en Event från Uppdraget: kopplar det till ett specifikt datum/tid.',
      'You add Teams to the Event: each team is a group of players.':
          'Du lägger till Lag i Eventet: varje lag är en grupp spelare.',
      'You share each Player a magic link: they tap, the event starts, they play.':
          'Du delar en magisk länk med varje Spelare: de trycker, eventet startar, de spelar.',
      'One assignment = one event. If you want to run the same adventure twice, buy two assignments.':
          'Ett uppdrag = en event. Om du vill köra samma äventyr två gånger, köp två uppdrag.',
      // assignments
      'Click "Store" in the sidebar.': 'Klicka på "Butik" i sidomenyn.',
      'Pick the adventure you want to run.': 'Välj äventyret du vill köra.',
      'Choose how many teams will play (1–100).': 'Välj hur många lag som ska spela (1–100).',
      'Click "Buy". You\'re sent to Stripe Checkout.':
          'Klicka på "Köp". Du skickas till Stripe Checkout.',
      'After payment, you\'re sent back to the dashboard with a "Schedule now" prompt on the new assignment.':
          'Efter betalningen skickas du tillbaka till översikten med en "Schemalägg nu"-uppmaning på det nya uppdraget.',
      '[Screenshot: the Store with adventure cards]':
          '[Skärmdump: Butiken med äventyrskort]',
      'Refunds: if you bought the wrong assignment, contact support. Refunds are admin-only — they touch real money.':
          'Återbetalningar: om du köpt fel uppdrag, kontakta supporten. Återbetalningar görs endast av administratör — de rör riktiga pengar.',
      // create run
      'Click "Events" in the sidebar, then "Schedule run".':
          'Klicka på "Event" i sidomenyn, sedan "Schemalägg event".',
      'Pick the assignment you want to use (only unused assignments are listed).':
          'Välj det uppdrag du vill använda (endast oanvända uppdrag listas).',
      'Give the event a name (eg. "Office team-build, Friday afternoon"). This is for your benefit; players don\'t see it.':
          'Ge eventet ett namn (t.ex. "Kontorets lagbygge, fredag eftermiddag"). Det är till för dig själv; spelarna ser det inte.',
      'Pick a date and time. You can also leave it blank and start the event manually later.':
          'Välj datum och tid. Du kan också lämna fältet tomt och starta eventet manuellt senare.',
      'Confirm the no-minors attestation, or pick the COPPA path if you\'re running for under-13s.':
          'Bekräfta intygande om "inga minderåriga", eller välj COPPA-vägen om du kör för under 13 år.',
      'Save. The event appears in the Events list with state "scheduled".':
          'Spara. Eventet dyker upp i Eventslistan med status "schemalagd".',
      '[Screenshot: the schedule-event dialog]':
          '[Skärmdump: dialogen för att schemalägga en event]',
      'A scheduled event auto-starts the moment its time arrives, even if you\'ve forgotten to click Start. Players who open their magic link at the scheduled minute get going immediately.':
          'En schemalagd event startar automatiskt när tiden infaller, även om du har glömt att klicka på Starta. Spelare som öppnar sin magiska länk vid schemalagd tid kommer igång direkt.',
      // teams
      'Adding a team': 'Lägg till ett lag',
      'Open the event\'s detail page from the Events list.':
          'Öppna eventets detaljsida från Eventslistan.',
      'Click "Add team".': 'Klicka på "Lägg till lag".',
      'Type the team name and the player names (one per line, or paste from a CSV).':
          'Skriv lagets namn och spelarnas namn (ett per rad, eller klistra in från CSV).',
      'Save. Each player slot is created with a fresh magic-link token.':
          'Spara. Varje spelarplats skapas med en ny token för magisk länk.',
      '[Screenshot: the add-team dialog]':
          '[Skärmdump: dialogen för att lägga till lag]',
      'Sharing magic links': 'Dela magiska länkar',
      'Each player\'s magic link is what they tap to join the event. The event-detail page has a "Team links" panel: copy individual links, copy a CSV of all team members, or copy a single "captain link" per team that the captain forwards to teammates.':
          'Varje spelares magiska länk är det de trycker på för att gå med i eventet. Detaljsidan har en panel för "Laglänkar": kopiera enskilda länkar, kopiera en CSV med alla lagmedlemmar eller kopiera en "kaptenslänk" per lag som kaptenen skickar vidare till lagkamraterna.',
      '[Screenshot: the team links panel with copy buttons]':
          '[Skärmdump: panelen för laglänkar med kopiera-knappar]',
      'Magic links are sensitive — anyone with the URL can play as that slot. Don\'t paste them into public Slack channels. The dashboard\'s "Rotate token" action mints a new link if one leaks.':
          'Magiska länkar är känsliga — den som har URL:en kan spela som den platsen. Klistra inte in dem i offentliga Slack-kanaler. Åtgärden "Rotera token" i översikten skapar en ny länk om en läckt.',
      'Token expiry': 'Tokenens giltighetstid',
      'Slot tokens expire seven days after the event\'s scheduled time by default. The team-links panel shows "expires in Nd" so you can spot links that need re-sharing.':
          'Platstokens går ut sju dagar efter eventets schemalagda tid som standard. Panelen för laglänkar visar "går ut om N dagar" så att du ser vilka länkar som behöver delas på nytt.',
      // running
      'Start, pause, resume': 'Starta, pausa, återuppta',
      'A scheduled event auto-starts at its time, but you can also start it manually from the event-detail page. Pause the event to freeze player progress (lunch break, weather delay), then Resume to continue.':
          'En schemalagd event startar automatiskt vid sin tid, men du kan också starta den manuellt från detaljsidan. Pausa eventet för att frysa spelarnas progress (lunchpaus, väderavbrott), och Återuppta för att fortsätta.',
      'The activity feed': 'Aktivitetsflödet',
      'The event-detail page has a live activity feed — answer attempts (right and wrong), team completions, gate reaches, state changes. Useful for noticing a stuck team in real time.':
          'Detaljsidan har ett liveflöde — svarsförsök (rätt och fel), färdiga lag, platsgrindar som nås, statusändringar. Bra för att upptäcka ett fastlåst lag i realtid.',
      '[Screenshot: activity feed during a live event]':
          '[Skärmdump: aktivitetsflödet under en pågående event]',
      'The live view': 'Livevyn',
      'Each event has a public-ish "live view" URL — a leaderboard plus optional audience map. Open it on a TV in the lobby; spectators can see team progress without logging in.':
          'Varje event har en (nästan) publik "live"-URL — en topplista plus en valfri publikkarta. Öppna den på en TV i lobbyn; åskådare kan se lagens progress utan att logga in.',
      'The map is OFF by default for privacy. Toggle it on from the event settings if your players are okay with their position being shown to the audience.':
          'Kartan är AV som standard för integritetens skull. Slå på den i eventets inställningar om dina spelare är okej med att deras position visas för publiken.',
      '[Screenshot: the live view leaderboard with map enabled]':
          '[Skärmdump: livevyns topplista med karta påslagen]',
      'Completion': 'Avslut',
      'When every team finishes, the event automatically transitions to "completed". Players see the outro text and a stats screen (right answers, distance walked, total time). The event row in your dashboard moves to the "completed" section.':
          'När alla lag är klara övergår eventet automatiskt till "avslutad". Spelarna ser outrotexten och en statistikskärm (rätta svar, gångavstånd, total tid). Eventets rad i översikten flyttas till sektionen "avslutade".',
      // webhooks (manual section)
      'Webhooks let you hook the platform up to your own systems — push events into Slack, trigger a Zapier flow, update a corporate dashboard, send parents a "your kid\'s team finished" SMS. The platform POSTs JSON to an HTTPS URL you control whenever something interesting happens.':
          'Webhooks låter dig koppla plattformen till dina egna system — skicka händelser till Slack, trigga ett Zapier-flöde, uppdatera en intern dashboard, skicka SMS till föräldrar när deras barns lag är klart. Plattformen POSTar JSON till en HTTPS-URL du kontrollerar när något intressant händer.',
      'When to use webhooks': 'När du vill använda webhooks',
      'Show a live leaderboard on a TV in your lobby, branded your way.':
          'Visa en topplista på en TV i lobbyn, med er egen branding.',
      'Notify parents (school events) when their child\'s team finishes.':
          'Meddela föräldrar (skolevent) när deras barns lag är klart.',
      'Trigger fulfillment in your own ops system when an event completes.':
          'Trigga utförande i ditt eget system när en event är klar.',
      'Pipe events into your analytics warehouse.':
          'Mata händelser till ert analyslager.',
      'The event catalog': 'Händelsekatalogen',
      'Seven event kinds in v1:': 'Sju händelsetyper i v1:',
      'a new event was created.': 'en ny event skapades.',
      'an event actually started (manual or auto-promote).':
          'en event faktiskt startade (manuellt eller automatiskt).',
      'all teams finished.': 'alla lag klara.',
      'a new team was added to an event.': 'ett nytt lag lades till i en event.',
      'a team finished. Includes their name and event id.':
          'ett lag är klart. Innehåller namnet och eventets id.',
      'an assignment was purchased.': 'ett uppdrag köptes.',
      'a purchase was refunded.': 'ett köp återbetalades.',
      'Configure an endpoint': 'Konfigurera en slutpunkt',
      'Click "Webhooks" in the sidebar.': 'Klicka på "Webhooks" i sidomenyn.',
      'Click "Add endpoint".': 'Klicka på "Lägg till slutpunkt".',
      'Click "Add team".': 'Klicka på "Lägg till lag".',
      'Give it a name, paste your URL, tick the events you want.':
          'Ge den ett namn, klistra in din URL, kryssa i händelserna du vill ha.',
      'Save. A secret is shown once — copy it now.':
          'Spara. En nyckel visas en gång — kopiera den nu.',
      '[Screenshot: the webhook create dialog with the secret reveal]':
          '[Skärmdump: dialogen för att skapa webhook med nyckelvisningen]',
      'Verifying signatures': 'Verifiera signaturer',
      'Every POST carries an X-Zv-Signature header of the form ':
          'Varje POST har ett X-Zv-Signature-huvud i formen ',
      ' — that\'s HMAC-SHA256 of the raw request body, keyed by your secret.':
          ' — det är HMAC-SHA256 av rå begärankropp, nyckel-låst med din nyckel.',
      'Verify in your handler before trusting the body:':
          'Verifiera i din hanterare innan du litar på kroppen:',
      'Always compare with a constant-time comparator (hmac.Equal in Go, timingSafeEqual in Node, hmac.compare_digest in Python). A naive == leaks bytes via timing.':
          'Använd alltid en jämförare med konstant tidsåtgång (hmac.Equal i Go, timingSafeEqual i Node, hmac.compare_digest i Python). Ett naivt == läcker bytes via tidsskillnader.',
      'Send a test': 'Skicka ett test',
      'Use the "Send test" button on each endpoint to fire a synthetic ':
          'Använd knappen "Skicka test" på varje slutpunkt för att skicka en syntetisk ',
      ' delivery. The customer-side handler can verify the signature without waiting for a real lifecycle event.':
          '-leverans. Hanteraren på kundsidan kan verifiera signaturen utan att vänta på en riktig livscykelhändelse.',
      '[Screenshot: the Send test button + a delivered toast]':
          '[Skärmdump: Skicka test-knappen och en leveransbekräftelse]',
      'Delivery and retry': 'Leverans och nya försök',
      'A delivery worker drains every 5 seconds. On any non-2xx response or timeout we retry with exponential backoff (1m → 5m → 30m → 2h → 8h → 24h, six attempts). After that the delivery is "dead-lettered" — no further retries.':
          'En leveransarbetare jobbar var 5:e sekund. Vid alla svar utanför 2xx eller timeout försöker vi igen med exponentiell väntetid (1m → 5m → 30m → 2h → 8h → 24h, sex försök). Därefter "dödbrev"-sätts leveransen — inga fler försök.',
      'Reply with a 2xx as quickly as you\'ve persisted the event; do the actual work async. The faster you reply, the fewer duplicates we have to enqueue.':
          'Svara med 2xx så snart du har sparat händelsen; gör det riktiga arbetet asynkront. Ju snabbare du svarar, desto färre dubbletter behöver vi köa.',
      'Dedupe': 'Avdubblering',
      'Use the X-Zv-Delivery-Id header (a 32-char hex) as your idempotency key. Duplicate deliveries during retries reuse the same id; insert it into a ':
          'Använd X-Zv-Delivery-Id-huvudet (32 hex-tecken) som idempotens-nyckel. Återförsök återanvänder samma id; lägg in det i en ',
      ' table with ON CONFLICT DO NOTHING and skip if it was already there.':
          '-tabell med ON CONFLICT DO NOTHING och hoppa över om det redan fanns.',
      'Rotating the secret': 'Rotera nyckeln',
      'If a secret leaks, click "Rotate secret" on the endpoint. A new secret is minted; the old one stops working immediately. Deploy the new value to your verifier first to avoid a brief gap of failed deliveries.':
          'Om en nyckel läcker, klicka på "Rotera nyckel" på slutpunkten. En ny nyckel skapas; den gamla slutar fungera direkt. Distribuera det nya värdet till din verifierare först för att undvika ett kort glapp av misslyckade leveranser.',
      // account
      'The Settings page has the operational controls for your own account.':
          'Inställningssidan har de operativa kontrollerna för ditt eget konto.',
      'Change password': 'Byt lösenord',
      'Requires the current password. Other sessions are signed out on success.':
          'Kräver det nuvarande lösenordet. Övriga sessioner loggas ut vid lyckad ändring.',
      'Change email': 'Byt e-post',
      'We send a confirmation link to the new address. The change only takes effect when you click it.':
          'Vi skickar en bekräftelselänk till den nya adressen. Ändringen träder i kraft först när du klickar på den.',
      'Download my data': 'Ladda ner mina uppgifter',
      'A single JSON file with your account info plus your organization\'s runs, teams, adventures, assignments and purchases. The GDPR-friendly export.':
          'En enda JSON-fil med din kontoinformation samt din organisations event, lag, äventyr, uppdrag och köp. Det GDPR-vänliga exportet.',
      'Delete account': 'Radera konto',
      'If you\'re the last member, your organization is deleted with you. All runs, teams, adventures, assignments. Cannot be undone.':
          'Om du är sista medlemmen raderas din organisation med dig. Alla event, lag, äventyr, uppdrag. Kan inte ångras.',
      // help
      'If something doesn\'t work as described here, or you\'re unsure how to do something, start with the event\'s activity feed (any errors are logged there for the team that hit them).':
          'Om något inte fungerar som beskrivet här, eller du är osäker på hur något görs, börja med eventets aktivitetsflöde (eventuella fel loggas där för laget som råkade ut för dem).',
      'For platform-level support, contact the operator listed at the bottom of the Privacy page.':
          'För plattformssupport, kontakta operatören som listas längst ner på Integritetssidan.',
      'This manual evolves. If you read something that\'s wrong or unclear, tell support — clearer docs help every other arranger after you.':
          'Den här handboken utvecklas. Om du läser något fel eller otydligt, säg till supporten — tydligare dokumentation hjälper alla andra arrangörer efter dig.',
      // ---- Round 2: connection / GPS / refunds / audit ----
      'Disconnected — event data may be stale.':
          'Frånkopplad — eventdatan kan vara inaktuell.',
      'Disconnected — leaderboard may be stale.':
          'Frånkopplad — topplistan kan vara inaktuell.',
      'Reconnecting — live updates paused.':
          'Återansluter — liveuppdateringar pausade.',
      'Map unavailable — leaderboard still live.':
          'Kartan otillgänglig — topplistan är fortfarande live.',
      'Offline — will send the moment you reconnect.':
          'Offline — skickas så fort du återansluter.',
      'Reconnected — your last answer was sent.':
          'Återansluten — ditt senaste svar har skickats.',
      "GPS hasn't locked yet. Wait a moment and tap Retry, or move outdoors.":
          'GPS har inte hittats än. Vänta en stund och tryck Försök igen, eller gå ut.',
      'Location is blocked. Open your browser or device settings, allow location for this site, then tap Retry.':
          'Plats är blockerad. Öppna webbläsarens eller enhetens inställningar, tillåt plats för den här webbplatsen, och tryck sedan Försök igen.',
      'No GPS signal. Move to open sky (away from buildings/trees) and tap Retry — usually clears in 30 seconds.':
          'Ingen GPS-signal. Gå ut under öppen himmel (bort från byggnader/träd) och tryck Försök igen — brukar lösa sig på 30 sekunder.',
      'Location off — see below': 'Plats av — se nedan',
      'Retry GPS': 'Försök igen',
      'Refund initiated. Confirming with Stripe…':
          'Återbetalning startad. Bekräftar med Stripe…',
      'Refund confirmed.': 'Återbetalning bekräftad.',
      'Refund still processing — check Stripe and refresh in a minute.':
          'Återbetalning behandlas fortfarande — kolla Stripe och ladda om om en minut.',
      'Audit event #{{id}}': 'Granskningshändelse #{{id}}',
      'Show details': 'Visa detaljer',
      'User-Agent': 'User-Agent',
      'Minimum 8 characters. Pick something the new user can change later in Settings.':
          'Minst 8 tecken. Välj något användaren kan byta senare i Inställningar.',
      'Cannot delete: {{n}} event(s) currently running. Stop them first.':
          'Kan inte ta bort: {{n}} event pågår just nu. Stoppa dem först.',
      'You are about to delete: {{members}} user(s), {{assignments}} assignment(s), {{players}} player(s), {{events}} event(s). Cascades immediately and cannot be undone.':
          'Du håller på att ta bort: {{members}} användare, {{assignments}} uppdrag, {{players}} spelare, {{events}} event. Sker direkt och kan inte ångras.',
      // ---- Round 3: lockout / sessions / audit / revert / TV / authoring ----
      'Too many failed attempts. Try again in {{n}}s.':
          'För många misslyckade försök. Prova igen om {{n}} s.',
      'You can try again now.': 'Du kan prova igen nu.',
      'Discard this adventure?': 'Kasta detta äventyr?',
      'Discard unsaved changes to this adventure?':
          'Kasta osparade ändringar i detta äventyr?',
      'Looks good.': 'Ser bra ut.',
      'Lowercase a–z, digits, and hyphens only.':
          'Endast små bokstäver a–z, siffror och bindestreck.',
      'This image is embedded in the puzzle prompt. Deleting will leave a broken image where it appears. Continue?':
          'Bilden är inbäddad i pusseltexten. Tas den bort blir det en trasig bild där den används. Fortsätt?',
      '1 event is running this adventure right now. Saving will change what those players see — typo fixes are safe; logic changes can confuse them mid-puzzle. Continue?':
          '1 event kör det här äventyret just nu. När du sparar ändras vad de spelarna ser — stavfel är ofarliga, logikändringar kan förvirra dem mitt i pusslet. Fortsätt?',
      '{{n}} events are running this adventure right now. Saving will change what those players see — typo fixes are safe; logic changes can confuse them mid-puzzle. Continue?':
          '{{n}} event kör det här äventyret just nu. När du sparar ändras vad de spelarna ser — stavfel är ofarliga, logikändringar kan förvirra dem mitt i pusslet. Fortsätt?',
      'Active sessions': 'Aktiva sessioner',
      "Every device you're signed into. If something looks unfamiliar, sign that session out.":
          'Alla enheter du är inloggad på. Logga ut sessionen om något ser obekant ut.',
      'No active sessions.': 'Inga aktiva sessioner.',
      'this device': 'den här enheten',
      'Unknown device': 'Okänd enhet',
      'Created {{when}}': 'Skapad {{when}}',
      'expires {{when}}': 'går ut {{when}}',
      'from {{ip}}': 'från {{ip}}',
      'Session signed out': 'Sessionen utloggad',
      'Sign out everywhere else': 'Logga ut överallt annars',
      "Sign out of every other browser, phone, and tablet? You'll stay signed in here.":
          'Logga ut från alla andra webbläsare, telefoner och surfplattor? Du förblir inloggad här.',
      'Other sessions signed out': 'Andra sessioner utloggade',
      'Recent security activity': 'Senaste säkerhetsaktivitet',
      'Your last 25 login, password and email-change events.':
          'Dina senaste 25 inloggnings-, lösenords- och e-poständringshändelser.',
      'No recent activity.': 'Ingen aktivitet nyligen.',
      'Recent change from {{prev}} → {{cur}}.':
          'Nyligen ändrad från {{prev}} → {{cur}}.',
      'You can revert until {{when}} if you typed the new address wrong.':
          'Du kan ångra fram till {{when}} om du skrev fel adress.',
      'Revert email change': 'Ångra e-poständring',
      'Revert your email back to {{prev}}?':
          'Återställ din e-post till {{prev}}?',
      'Email reverted to {{email}}.': 'E-post återställd till {{email}}.',
      'We send a confirmation link to the new address — your email only changes when you click it. After confirming you have a {{n}}-day window to revert if you typed it wrong.':
          'Vi skickar en bekräftelselänk till den nya adressen — e-posten ändras först när du klickar. Efter bekräftelse har du {{n}} dagars ångerfönster om du skrev fel.',
      'Event complete': 'Eventet är klart',
      'Event complete · press B to show the leaderboard again · ':
          'Eventet klart · tryck B för att visa topplistan igen · ',
      'Thanks for watching.': 'Tack för att du tittade.',
      'crossed the line in {{time}}': 'gick i mål på {{time}}',
      'finished first': 'kom först i mål',
      'no winner recorded': 'ingen vinnare registrerad',
      '1 other team played.': '1 annat lag spelade.',
      '{{n}} other teams played.': '{{n}} andra lag spelade.',
      'Ready': 'Redo',
      "waiting on the arranger's signal": 'väntar på arrangörens startsignal',
      '1 team ready': '1 lag redo',
      '{{n}} teams ready': '{{n}} lag redo',
      // ---- Round 4: a11y / standardization ----
      'Skip to main content': 'Hoppa till huvudinnehåll',
      'Open navigation menu': 'Öppna navigeringsmenyn',
      'Primary': 'Huvudnavigering',
      'Sidebar': 'Sidopanel',
    },
  };

  // LANG_LABEL is the picker text. Each entry is { flag, name } —
  // flag is a Regional-Indicator-pair emoji (renders as a country
  // flag on systems with emoji-flag fonts), name is the language's
  // own native name. English uses 🇬🇧 because the product is EU-
  // facing; swap to 🇺🇸 for an Americas launch.
  const LANG_LABEL = {
    en: { flag: '🇬🇧', name: 'English' },
    sv: { flag: '🇸🇪', name: 'Svenska' },
  };

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
  // languages. Each option renders as "🇬🇧 English" (flag + native
  // name) so users can spot their language without reading. Styling
  // is up to the caller — the sidebar footer adds the "ghost"-style
  // classes to match its siblings.
  function makeLangPicker(opts = {}) {
    const sel = document.createElement('select');
    sel.className = opts.className || 'zv-lang-picker';
    if (opts.title) sel.title = opts.title;
    for (const code of available()) {
      const label = LANG_LABEL[code];
      const o = document.createElement('option');
      o.value = code;
      o.textContent = label ? label.flag + ' ' + label.name : code;
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
