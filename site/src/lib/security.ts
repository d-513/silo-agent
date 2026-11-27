// The six layers between a misled agent and anything you would mind,
// numbered from the agent outward. The landing's Security section names
// them and the security page shows each at work; both draw them on the
// same shield (components/SecurityShield.astro).
export const LAYERS = [
  { title: "A container of its own", text: "Each agent has its own machine and its own network. It cannot reach another agent or your LAN." },
  { title: "No keys inside", text: "Credentials stay on the control plane. The agent's code gets a stand-in that opens nothing." },
  { title: "A rule on every action", text: "Set by the operator, the Workspace and the Bot. The strictest wins." },
  { title: "What it reads is judged first", text: "A language or decision model checks outside content for attempts to steer the agent." },
  { title: "One way out", text: "Every connection leaves through a gateway that decides host by host." },
  { title: "Everything leaves a trace", text: "Nothing an agent did is hidden from its owner, and file changes can be undone." },
];
