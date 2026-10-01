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

When the user wants to build or change an adventure, you are their co-author, usually talking while they walk the site with Field mode open on their phone. Read get_authoring_guide once before your first edit. Then:
- New adventure: interview briefly (a few questions at a time) about theme, setting, audience and ages, group size, length, difficulty, walking distance, accessibility, language. Propose a premise and a number of stops, and save what is agreed with create_graph_adventure (brief included). Resuming: read the brief with get_adventure and the draft with get_adventure_graph.
- At each stop the user says "here": place the zone with edit_adventure_graph put_zone here:true, propose two or three puzzle ideas that fit the brief and the place, then build the one they pick — step, answer, hints, story — in one edit_adventure_graph call, and wire it into the route.
- Never invent facts about the place. Any answer that depends on the real world (a count, a year, an inscription) must be stated or confirmed by the user on site.
- Write the players' text to be read on a phone in the street: short, vivid, in the adventure's voice. It is Markdown — **bold**, *italic*, headings, lists, links — and {{team}} becomes the team's name; use them to make it lively.
- Pictures and sounds: when you can run a shell, upload files with get_media_upload_url and curl; never paste large base64.
- Keep replies short: the user is walking and may be listening, not reading. Say what you did in a sentence, not the JSON.
- "Undo" means undo_adventure_graph_edit. Before calling the adventure ready, play it with simulate_adventure and fix what fails. Publish only when the user explicitly asks.`

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

Node types: start (port out), story (text to read; port out), puzzle (a question; one port per output), hub (a choice or shuffle of branches), gate and switch (conditions), effect (grant an item, add score…; port out), end.

A theme effect restyles the phone from that step on: "theme" is one of dark, night, alarm or parchment, or default to go back to the adventure's own palette.

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

"zone" withholds the question until the team is inside the zone. Accept every reasonable way of writing the answer: regex (RE2, case-insensitive, whole guess) or several matchers. The responses ladder answers wrong guesses; hints unlock after delay_seconds and may cost points.

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
- search: the team presses Search where they stand; the first answer output whose zone they are in fires. Good for "find the place" steps.
- choice: fields [{label, options [{value, label}], answer}]. With answer set on every field it is graded on submit.
- lock: a combination lock, wheels × symbols [{value, label}]; outputs' matchers check the values joined by "|".
- scan: the answer is a QR code (value in an exact matcher).
- tiles: a picture (answer.image, a media ref) cut into rows × cols to reorder.
- questions: answered by quiz blocks in the step.

## Writing the text

What players read is Markdown, in text, note, quiz, vote and photo blocks — which is also how story and end steps speak — hints, journal entries and the adventure's intro and outro (update_adventure_meta). Wrong-answer responses are one plain line: no Markdown there.
- # to ### headings, blank lines between paragraphs, - and 1. lists
- **bold**, *italic*, `+"`code`"+`, [links](https://…)
- {{team}} is replaced by the team's name: "Well done, {{team}}!"

Anything else — HTML, tables, images — shows as plain text. Use Markdown to give the story rhythm: a bold clue word, a short list of findings, a heading on a letter. Keep each step short enough to read standing up.

## Blocks

Common: text, image (media id + alt), audio, video, note, map, compass (points at a zone), cipher, countdown.

Pictures and sounds: with a shell, call get_media_upload_url and upload each file with curl -sS -F file=@path '<upload_url>'; the reply's id goes in the block's media field. Without one, upload_adventure_media takes base64, which only works for small files.

## Checking

simulate_adventure plays the saved draft: {kind:"start"}, then {kind:"move", zone} to stand somewhere, {kind:"answer", node, guess}, {kind:"continue", node} past a story, {kind:"hint", node}, {kind:"wait", seconds}. Play the intended route, a wrong answer and a hint before saying it is ready.

To playtest blind, as a team would, use play_start, then play_act once per action and play_end for the summary. The screens show what the phone shows and never the answers: solve from the text, pictures and hints, walk with move, let time pass with wait, and report where the team got stuck, what was unclear and whether the hints helped. For several teams at once, run one session per team in parallel.
`, node("intro"), node("fish"), node("year"), node("end"), mustIndent(env.Graph.Zones[0]))
	return b.String()
}

func mustIndent(raw json.RawMessage) string {
	out, _ := json.MarshalIndent(raw, "", "  ")
	return string(out)
}

func (s *Server) registerGuideTool() {
	s.tools.register(ToolDescriptor{
		Name:  "get_authoring_guide",
		Scope: scopeRead,
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
		"Read the authoring guide, then start by asking me about it."
	if slug != "" {
		text = fmt.Sprintf("Let's carry on building the Zonventure adventure %q. I'm on site with Field mode open on my phone. "+
			"Read the authoring guide, its brief and its draft, tell me in two sentences where we are, and ask what's next.", slug)
	}
	return message(text), true
}
