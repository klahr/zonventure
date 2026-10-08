/**
 * A random team name for the join page's dice: an adjective and an
 * animal, in the page's language. Kind words only — the players are
 * often children.
 */
const names = {
  en: {
    adjectives: ["Speedy", "Brave", "Happy", "Clever", "Wild", "Mighty", "Secret", "Tricky", "Shining", "Flying", "Sneaky", "Golden", "Cheeky", "Fearless", "Silent", "Smart"],
    animals: ["Foxes", "Owls", "Bears", "Wolves", "Hedgehogs", "Squirrels", "Hares", "Moose", "Frogs", "Dolphins", "Tigers", "Dragons", "Pandas", "Penguins", "Lynxes", "Beavers", "Falcons", "Seals", "Rabbits"],
    format: (adjective: string, animal: string) => `The ${adjective} ${animal}`,
  },
  sv: {
    adjectives: ["snabba", "modiga", "glada", "listiga", "vilda", "starka", "hemliga", "kluriga", "lysande", "flygande", "busiga", "gyllene", "tappra", "tysta", "smarta", "hoppande"],
    animals: ["rävarna", "ugglorna", "björnarna", "vargarna", "igelkottarna", "ekorrarna", "hararna", "älgarna", "grodorna", "delfinerna", "tigrarna", "drakarna", "pandorna", "pingvinerna", "lodjuren", "bävrarna", "falkarna", "sälarna", "kaninerna"],
    format: (adjective: string, animal: string) => `De ${adjective} ${animal}`,
  },
};

const pick = (list: string[]) => list[Math.floor(Math.random() * list.length)]!;

/** A team name in locale (English when there is no list for it), never the same as previous. */
export function randomTeamName(locale: string, previous = ""): string {
  const n = names[locale as keyof typeof names] ?? names.en;
  for (;;) {
    const name = n.format(pick(n.adjectives), pick(n.animals));
    if (name !== previous) return name;
  }
}
