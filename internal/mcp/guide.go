package mcp

// How an assistant is meant to build an adventure with its author, in
// three layers so the author never has to explain it:
//
//   - serverInstructions go out in the initialize reply, which clients
//     put in the model's context for the whole session: the few rules
//     that always hold.
//   - the field_session prompt starts a session in one pick, from a
//     client's prompt or slash-command menu.
//   - get_authoring_guide returns the long reference — step recipes as
//     node JSON — for the model to read once per session.
//
// guideExample is a real graph: guide_test.go parses it strictly and
// validates it, so the recipes cannot drift from what the server takes.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const serverInstructions = `Zonventure builds location-based adventures: teams walk between real places and solve a puzzle at each.

You are the user's co-author. They are often on site, walking, phone in hand. Be brief and focused:
- Do what was asked, then say what changed in one short sentence. No recaps, no JSON, no restating their request, no praise.
- Ask only what you need to go on, one question at a time. Offer a single sensible default, not a menu; give options only when they ask or a choice is genuinely theirs to make (a premise, a puzzle).
- Do not explain the plan or name steps unless they seem lost or ask "what now?".
Read get_authoring_guide once before your first edit.

A new adventure, in order:
1. The idea: what it is about, who it is for, how long, which language — a few short questions; then propose one or two premises.
2. Make it: create_adventure with the agreed brief, then create_dev_ticket and put its link in the chat so their phone follows every save.
3. The look: if list_org_themes has a fitting theme, suggest it; otherwise propose one look. Apply with edit_adventure_graph put_theme and main: true.
4. Where it starts: update_adventure_meta, from get_creator_position when they stand there.
5. The stops: when they say "here", place the zone with edit_adventure_graph put_zone here:true, suggest two puzzle ideas for the place, and build the chosen one — step, answer, hints, story — wired into the route, in one edit_adventure_graph call. dev_go_to shows it on their phone.
6. Try it: simulate_adventure and fix what fails; dev_reset starts their phone over. Publish only when they explicitly ask.

Editing:
- "This" is the step on their phone: get_dev_screen.
- Change words with edit_adventure_graph set_text {find, replace}, quoting the text exactly; an answer, a hint or a block with patch_node {patch} (outputs merge by port, blocks and matchers by id: send only what changes). Without slug both edit the phone's step, which updates within a second. Read one step with get_step, not the whole graph; use put_node only to rebuild a step.
- "Undo" means undo_adventure_graph_edit.
- Carrying on with an adventure: get_adventure for the brief, get_adventure_graph for the draft; say in one sentence where it stands.

Content:
- Never invent facts about the place. Answers that depend on the real world (a count, a year, an inscription) must come from the user on site.
- Players read on a phone in the street: short, vivid, in the adventure's voice. Markdown works — **bold**, *italic*, headings, lists, links — and {{team}} becomes the team's name.
- Pictures and sounds: get_upload_link and write the link in the chat; the user taps it and picks the file (with a shell, curl it yourself). For a step's picture, list_adventure_media then gives its id. for: icon sets the adventure's icon. Never paste large base64.

Events (the adventure must be published first, with publish_adventure_graph — only when the user asks):
- Self-guided, sold in the store: create_event {adventure_slug, public_for_sale: true, attest_no_minors: true}, update_event with ticket_price_cents, ticket_currency, ticket_team_size and public_blurb (and translations, its name, blurb and meeting place in the adventure's other languages), then start_event. It shows in the store once a platform admin approves the listing (at once for a verified creator); get_event's listing_review tells. give_free_ticket sends someone a ticket.
- Scheduled, for invited teams: create_event with the adventure_slug. Nothing is paid up front: each team is charged to the organisation as it starts (49 kr a team, set in Settings → Pricing), from its balance on the console's Billing page. Then create_team per team (with their email) and send_event_invitations, or give_free_ticket per person; start_event when it begins.
- An event's cover: get_upload_link {for: event_cover, event_id}.
- Printed letters: an adventure's print materials (PDFs) are posted, emailed or kept for organisers; set_print_delivery changes it for one event. list_orders shows the gift boxes and store purchases still to post, with each envelope's language; mark_order_posted once one is sent.`

// guideExample is a small adventure using the common step types, wired
// start to end. The guide shows its nodes as recipes.
const guideExample = `{"format":"zonventure-adventure-v2","graph":{
"zones":[
  {"slug":"fountain","name":"The fountain","lat":59.3293,"lng":18.0686,"radius_m":30},
  {"slug":"church","name":"St. Clara church","lat":59.3321,"lng":18.0612,"radius_m":30}
],
"nodes":[
  {"slug":"start","type":"start"},
  {"slug":"intro","type":"story","title":"The letter","story":{"blocks":[
    {"id":"b1","kind":"text","text":"A letter was found in the old harbour office. It speaks of a treasure — and of a fountain."}]}},
  {"slug":"fish","type":"puzzle","title":"Stone fish","puzzle":{
    "zone":"fountain",
    "blocks":[{"id":"b1","kind":"text","text":"How many stone fish guard the fountain?"}],
    "outputs":[{"port":"right","label":"Right","trigger":"answer","points":10,"matchers":[
      {"id":"seven","kind":"regex","value":"(7|seven)"}]}],
    "responses":[{"text":"Count again — some hide on the far side."},{"after_attempts":3,"text":"Walk all the way around."}],
    "hints":[{"text":"Look below the rim, too.","delay_seconds":120},{"text":"There are more than five.","delay_seconds":240,"cost":2}]}},
  {"slug":"year","type":"puzzle","title":"The year","puzzle":{
    "zone":"church",
    "blocks":[{"id":"b1","kind":"text","text":"Above the door is the year the church was finished. Which?"}],
    "bar":{"keyboard":"number"},
    "outputs":[
      {"port":"right","label":"Right","trigger":"answer","points":10,"matchers":[{"id":"y","kind":"interval","low":1755,"high":1755}]},
      {"port":"giveup","label":"Gave up","trigger":"skipped"}],
    "skip_after_guesses":5,
    "hints":[{"text":"It's carved in Roman numerals.","delay_seconds":90}]}},
  {"slug":"end","type":"end","end":{"outcome":"complete","blocks":[
    {"id":"b1","kind":"text","text":"The treasure was the city all along. Well done!"}]}}
],
"edges":[
  {"from":"start","from_port":"out","to":"intro"},
  {"from":"intro","from_port":"out","to":"fish"},
  {"from":"fish","from_port":"right","to":"year"},
  {"from":"year","from_port":"right","to":"end"},
  {"from":"year","from_port":"giveup","to":"end"}
]}}`

func authoringGuide() string {
	var env struct {
		Graph struct {
			Nodes []json.RawMessage `json:"nodes"`
			Zones []json.RawMessage `json:"zones"`
		} `json:"graph"`
	}
	_ = json.Unmarshal([]byte(guideExample), &env)
	node := func(slug string) string {
		for _, n := range env.Graph.Nodes {
			var s struct {
				Slug string `json:"slug"`
			}
			_ = json.Unmarshal(n, &s)
			if s.Slug == slug {
				out, _ := json.MarshalIndent(json.RawMessage(n), "", "  ")
				return string(out)
			}
		}
		return ""
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w(`# Building an adventure

An adventure is a graph. Nodes are steps; edges run from a node's output port to the next node. Zones are places on the map, referenced by slug. Every path must lead from the one start node to an end node.

Node types: start (port out), story (text to read; port out), puzzle (a question; one port per output), hub (a choice or shuffle of branches), gate and switch (conditions), effect (grant an item, add score…; port out), split and join (parallel paths), end.

A hub's branches lead back to it, and it leaves by its done port once enough are done (required). For a fork in the road, where the team picks one way and carries on, set the hub's one_way: true: its branches lead on and never back, and it has no done port.

The adventure's look is its main theme: the graph's "main_theme" names one of its saved themes, in "themes": {slug, name, look}, a whole look (colours, fonts, corners, cards, backdrop, style). Add or change one with edit_adventure_graph put_theme, with main: true to make it the main theme; like the rest of the draft it reaches players when published. A theme effect restyles the phone from that step on: "theme" is the slug of another saved theme, or default to go back to the main theme. A look's style.transition (none, fade, slide, zoom, flip, pop) sets how each step comes in.

Print materials are PDFs that come with the adventure on paper, such as a letter with the first clue that the organiser posts to the buyer; players never see them in the game. Upload each PDF with get_upload_link or upload_adventure_media, then add it with edit_adventure_graph put_print_material {id, name, delivery, files}: delivery is posted (the organiser prints and posts it), emailed (the buyer prints it) or organiser (for the organiser alone), and files maps each language the adventure is played in to its PDF's media id. A posted material missing a language can't be published.

Any block may move: "animation": {"entrance": one of fadeIn, fadeInUp, fadeInDown, slideInLeft, slideInRight, zoomIn, bounceIn, flipInX, backInUp, lightSpeedInRight, rollIn; "attention": one of pulse, bounce, shakeX, wobble, tada, heartBeat, swing, rubberBand, jello, flash, headShake; "delay": seconds 0–10, so blocks can arrive one after another; "duration": seconds per run, 0.2–5 (default 1); "repeat": three or forever, for the attention effect}. Use it sparingly — a bounce on the answer that matters, a slow pulse on a clue — and never as decoration on every block.

## Working with edit_adventure_graph

- Send the whole node in put_node; it replaces the node with that slug. Slugs are short, lowercase, hyphenated and stable.
- Wire as you go: to put a new step between A and B, put_node it, connect A's port to it, and connect its port to B.
- The reply lists the validation report. Fix errors; warnings are advice.
- Block ids ("b1", "b2", …) must be unique within a node. Matcher ids name an accepted answer.
- People may be watching the designer as you work: the steps you touch light up for them. Give each edit a short note ("adding hints to the riddle"); they see it beside your badge.

## Working with the authors

- The authors talk in threads on the steps. Read them before changing a step (list_adventure_threads with node): they say what was decided.
- A thread with for_assistant: true asks you for something. Do it, reply on the thread with what you did (comment_on_adventure with thread_id), and resolve it. Ask on the thread instead when it is unclear.
- Leave review notes and questions as threads on the step they are about, not as changes. @name mentions an author.
- To change a step an author owns or is working on, propose it with suggest_step_change: they see the change field by field and accept or reject it.
- set_step_status says where a step is: put a step you finished on review for an author to check; ready is theirs to give.

## A story step

%s

## A puzzle at a place, typed answer

"zone" withholds the question until the team is inside the zone. Accept every reasonable way of writing the answer: regex (RE2, case-insensitive, whole guess) or several matchers. The responses ladder answers wrong guesses; hints unlock after delay_seconds and may cost points. "quiet": true says neither right nor wrong, as on a quiz walk: any answer moves the team on, a wrong one by the first answer output for no points, and the results still show which were right.

%s

## A number, with a way out

interval accepts a number range (low = high for one number); near gives partial credit around a target (target, tolerance). bar.keyboard "number" shows a number pad. A skipped output, with skip_after_guesses and/or skip_after_seconds (both must be met when both are set), lets a stuck team move on.

%s

## The end

%s

## Zones

put_zone with here: true centres the zone where the author stands, sized from the GPS accuracy (15, 30 or 60 m); give it a slug and a name. A zone looks like:

%s

## Other answer kinds (puzzle.answer.kind)

Leave answer out for a typed answer. Otherwise:
- search: the team presses "We're here" where they stand; the first answer output whose zone they are in fires. Good for "find the place" steps.
- choice: fields [{label, options [{value, label}], answer}]. With answer set on every field it is graded on submit.
- lock: a combination lock, wheels × symbols [{value, label}]; outputs' matchers check the values joined by "|".
- scan: the answer is a QR code (value in an exact matcher).
- tiles: a picture (answer.image, a media ref) cut into rows × cols to reorder.
- order: cards [{text, color "#rrggbb", image (a media ref)}], 2 to 8, listed in their right order; start is the order they are dealt in, as card indices ([2,0,1]), reversed if left out. The team drags them into order.
- questions: answered by quiz blocks in the step.

## Parallel paths: split and join

A split divides the team into groups that go their own ways and meet again at a join: one group to the church for the key, the other to the well for the map, the lock opened together after. Items, notes, points and answers stay the team's, so what one group finds the other can use.

{"slug": "split-up", "type": "split", "title": "Split up", "split": {"text": "Two of you to the church, the rest to the well.", "paths": [{"port": "church", "role": "scout", "label": "Church party"}, {"port": "well", "label": "Well party"}]}}
{"slug": "meet", "type": "join", "title": "Meet at the square"}

- One port per path. A path with a role takes that role's player (a default role's: everyone holding no other role); everyone else is shared out evenly among the paths, so a big team with no roles splits into even groups. A path may not be a secret role's.
- Wire each path's steps from the split's port to the join, and the join's out to what the team does together. The paths may not share a step, and every path must lead to the same join (or end the adventure, which ends it for every group).
- Splits do not nest: a path may not lead to another split, nor back into its own, before its join. Meet at the join first; the team may split again after it.
- Each phone sees its own group's steps; a step done together (keys, unanimous) waits for the group; a role no one in the group holds is everyone's in it. A player in several groups — a small team — walks them one after another; a group with no one in it is walked by whoever has finished theirs.
- simulate_adventure and a playtest play the groups as one player alone would: continue past the split for the first group and play its path; walk {port: <group key>} turns to another group not yet waiting at the join.

## Writing the text

What players read is Markdown, in text, note, chat (each line), quiz, vote and photo blocks — which is also how story and end steps speak — hints, journal entries and the adventure's intro and outro (update_adventure_meta). Wrong-answer responses are one plain line: no Markdown there.
- # to ### headings, blank lines between paragraphs, - and 1. lists
- **bold**, *italic*, `+"`code`"+`, [links](https://…)
- {{team}} is replaced by the team's name: "Well done, {{team}}!"
- {{answer:<step>}} is the answer the team gave at that step: ask a name once, sign every later letter with it.

Anything else — HTML, tables, images — shows as plain text. Use Markdown to give the story rhythm: a bold clue word, a short list of findings, a heading on a letter. Keep each step short enough to read standing up.

## Blocks

Common: text, image (media id + alt), audio, video, note, map, compass (points at a zone), cipher, countdown.

Items can be opened from the team's bag: give an item blocks with put_item — a note block for a letter, an image for a map, audio for a recording — and a team that has it can look at it any time. Text in them is Markdown and translated like a step's.

Pictures and sounds: call get_upload_link and give the user the link to tap and pick the file (or, with a shell, curl -sS -F file=@path '<link>' and read the id from the reply); list_adventure_media lists what was uploaded, newest first. The id goes in the block's media field. upload_adventure_media takes base64, which only works for small files.

## Checking

simulate_adventure plays the saved draft: {kind:"start"}, then {kind:"move", zone} to stand somewhere, {kind:"answer", node, guess}, {kind:"continue", node} past a story, {kind:"hint", node}, {kind:"wait", seconds}. Play the intended route, a wrong answer and a hint before saying it is ready.

To playtest blind, as a team would, use playtest_start, then playtest_act once per action and playtest_end for the summary. The screens show what the phone shows and never the answers: solve from the text, pictures and hints, walk with move, let time pass with wait, and report where the team got stuck, what was unclear and whether the hints helped. For several teams at once, run one session per team in parallel.
`, node("intro"), node("fish"), node("year"), node("end"), mustIndent(env.Graph.Zones[0]))
	return b.String()
}

func mustIndent(raw json.RawMessage) string {
	out, _ := json.MarshalIndent(raw, "", "  ")
	return string(out)
}

func (s *Server) registerGuideTool() {
	s.tools.register(ToolDescriptor{
		Name:     "get_authoring_guide",
		Audience: audienceAuthor,
		Scope:    scopeRead,
		Description: "How to build an adventure graph step by step: node types and ports, recipes for common steps as exact node JSON " +
			"(story, zone-gated puzzle, number answer, end), answer kinds, blocks and how to playtest. Read it once before the first edit_adventure_graph call.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(context.Context, *Server, json.RawMessage) (ToolCallResult, error) {
		return textResult(authoringGuide()), nil
	})
}

// Prompts: ready-made starts a client lists in its prompt or slash
// command menu.

type promptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type promptDescriptor struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Arguments   []promptArgument `json:"arguments,omitempty"`
}

var prompts = []promptDescriptor{{
	Name:        "field_session",
	Title:       "Build an adventure on site",
	Description: "Start building an adventure together — a new one, or carry on with an existing one — while walking the place with Field mode open.",
	Arguments:   []promptArgument{{Name: "slug", Description: "An existing adventure to carry on with. Leave empty to start a new one."}},
}, {
	Name:        "review_adventure",
	Title:       "Review an adventure",
	Description: "Go through an adventure as a careful playtester and leave review notes on its steps, for the authors to resolve.",
	Arguments:   []promptArgument{{Name: "slug", Description: "The adventure to review.", Required: true}},
}, {
	Name:        "pick_up_tasks",
	Title:       "Do what the authors asked",
	Description: "Work through the threads where the authors asked the assistant (@assistant) for something, then report back on each.",
	Arguments:   []promptArgument{{Name: "slug", Description: "The adventure.", Required: true}},
}}

type promptMessage struct {
	Role    string      `json:"role"`
	Content ToolContent `json:"content"`
}

// getPrompt renders a prompt with its arguments, or reports it unknown.
func getPrompt(name string, args map[string]string) (map[string]any, bool) {
	slug := strings.TrimSpace(args["slug"])
	var p *promptDescriptor
	for i := range prompts {
		if prompts[i].Name == name {
			p = &prompts[i]
		}
	}
	if p == nil {
		return nil, false
	}
	message := func(text string) map[string]any {
		return map[string]any{
			"description": p.Description,
			"messages":    []promptMessage{{Role: "user", Content: ToolContent{Type: "text", Text: text}}},
		}
	}
	switch name {
	case "review_adventure":
		return message(fmt.Sprintf("Review the Zonventure adventure %q as a careful playtester would. Read the authoring guide, its brief, "+
			"its draft and its open threads (list_adventure_threads), and simulate a run or two. Then leave one thread per real issue with "+
			"comment_on_adventure, on the step it is about: answers too narrow to accept reasonable spellings, clues that give the answer away "+
			"or cannot be solved from what the team has, jumps in difficulty, hints that do not help, pictures without descriptions, text not "+
			"translated, places that are hard to reach. Say what is wrong; where the fix is clear, propose it with suggest_step_change "+
			"rather than describing it. Do not change the draft yourself. Skip what an open thread already raises. Finish with one thread on the whole adventure summing up, and tell me how many notes you left.", slug)), true
	case "pick_up_tasks":
		return message(fmt.Sprintf("The authors of the Zonventure adventure %q have asked you for things in its threads. List them with "+
			"list_adventure_threads (tasks_only), read the authoring guide if you have not, and do each in turn: make the change, reply on the "+
			"thread saying what you did, and resolve it. If a task is unclear or you disagree, reply asking instead and leave it open. "+
			"Tell me what you did when you are through.", slug)), true
	}
	text := "Let's build a new Zonventure adventure together. I'm on site with Field mode open on my phone. " +
		"Read the authoring guide, then start with your first question about the idea."
	if slug != "" {
		text = fmt.Sprintf("Let's carry on building the Zonventure adventure %q. I'm on site with Field mode open on my phone. "+
			"Read the authoring guide, its brief and its draft, tell me in one sentence where we are, and ask what's next.", slug)
	}
	return message(text), true
}
