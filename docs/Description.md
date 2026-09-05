# Silo Agent

A token-conserving, multi-user, containerized, secure Agent for completing various tasks from personal to teams etc.

## Stack

1. The agent shall use GoLang for the backends[Control Plane], and PNPM+React+Tailwind for frontend, and Go for the worker.
2. ConnectRPC (?) for all comms between these
3. Probably use GORM for the database. for now SQLITE
4. Session auth. OIDC later - keep this extendable.
5. Koanf for config. Env vars -> YAML config -> JSON settings.json Hierarchy, that would be editable using web UI
6. Python for the Agent itself to execute.

## Idea

The 'box' that is one agent and it's machine etc will be called a Bot.
One user can have multiple Bots. Bots don't share state between them by default.

Each Bot runs a durable Docker container. It is started by the Control Plane, via Docker API. For now only one docker will be used - the local one, in the future we may add option for multiple.
This container includes a GUI [determine - wayland or X11]. A linux container with a gui and basic programming tools. Likely need s6 or similar inside the container, but I still feel this is better than trying to run separate VMs or micro services for the actual Bot.
The user can accecss the bot container's GUI via the Web UI and see what it's doing. This requires some kind of Web VNC shenaningans.
The goal: Eg bot opens chrome, goes to website. Needs to login or captcha, The user can step in and do it.

Inside this container, runs a Worker.
Container gets some automated token passed via Env var, the worker connects and authenticated using it to the control plane via ConnectRPC and starts receiving queries.
/workspace is mounted on the host, this is where the agent is supposed to store files.

Web UI Displays all bots the user has access to. Also an admin UI for changing various settings.

## Tool types

Let's call things the agent can do tools.
These tools run inside the Bot's machine
We will need to translate them into the upstream AI Provider's definitions.
This needs a solid structure.
Most should be streamed

### Terminal

Terminal commands. Control plane sends to worker, worker executes.
Streaming. Long-running commands send status to LLM every few seconds.

### Files

read, write, patch, ripgrep, etc. Standard agentic stuff.

### Web

web search, web extract. Will be done via Firecrawl.

### Python & MCPs & Secrets - the real deal

We will call them Connectors in the UI. They are defined in the UI dynamically , not a pre-set.
Use MCP code execution with python. https://www.anthropic.com/engineering/code-execution-with-mcp
We would auto-generate a `tools` python library using the worker. Agents could then discover the connected tools themselves
Place eg /opt/tools inside container?
This call should likely go through the worker inside the Bot, rather than the generated tools automatically calling control plane, but this is up for consideration.
The Control Plane then handles the actual authenticated request. This should go through some sort of security engine, that can let the user define rules or manual allow per bot per connector action.
Another tool that we will have, is `secrets`. This will fetch a secret from the bot's page secret store.
This is separate from the secrets used for configuring MCPs.
Fetching a secret requires approval/auto-approve rule.
This allows the Bot to eg use a python IMAP library to simply access a user's mail, while also not relying on a fragile workflow like the user putting a password in a location that the LLM API sees, compromising security.

Connectors shouldn't just be MCP. `generated-mcp` connctors are just one type, but I think we should allow python ones too - it will mostly just be a front for adding a secret and the Worker cloning a repo with the connector

### Chromium Use

Possibly Playwright. It should be not headless, and the same browser that is opened in the Bot's GUI.
Architecting this is a TODO. ideally via the same python logic, but this time it doesnt need to call any outside tools, just the local instance.

### Computer Use

For later. Perceive -> Reason -> Act -> Verify Loop. Manipulates the GUI.

#### Minor comment

Since the LLM needs to see images, we probably need some kind of standard output things like <llm_image>base64 image</llm_image> and parse in outputs of commands etc, or some other approach, so that we can then take this, strip this from the text and upload it as an actual image to the API
