# SDD Agent Orchestra

A Go-based system for orchestrating AI agents to automate various stages of spec driven development, including requirements gathering, design, implementation, testing, and task management.

## Features

- Agent-based architecture for modular task execution
- Integration with OpenAI models for AI-powered decision making
- Support for MCP (Model Context Protocol) tools
- Configurable prompts, rules, and tools
- Artifact logging for traceability

## Installation

1. Ensure you have Go 1.26.1 or later installed.
2. Clone the repository:
   ```bash
   git clone https://github.com/nielsgts/sdd-agent-orchestra.git
   cd sdd-agent-orchestra
   ```
3. Install dependencies:
   ```bash
   go mod download
   ```
4. Build the project:
   ```bash
   go build
   ```

## Configuration

The system uses a `config.json` file to define directory paths and settings.

The agents are not fixed but can be changed and added.

## Usage

Run an agent with:

```bash
./sdd-agent-orchestra -config config.json <agent_name> [parameters]
```

For example, to run the requirements-new-feature agent:

```bash
./sdd-agent-orchestra requirements-new-feature -name "my-feature" -message "Add user authentication"
```

The default config expects a workflow in this order:
- requirements-new-feature or requirements-new-feature-from-source
- design-for-feature
- tasks-for-feature
- tests-for-feature
- implementation-for-feature
- requirements-merge-feature
- design-merge-feature

## Agents

Agents are defined in the `.sdd/agents/` directory as JSON files. Each agent specifies:

- Description and parameters
- Prompt files
- Input/output paths
- Available tools
- Model to use

## Models

Model configurations are in `.sdd/models/` directory, specifying API keys, endpoints, etc.

## Tools

Tools are defined in `.sdd/tools/` directory, supporting internal Go functions or MCP servers.

## Testing

Run tests with:

```bash
go test
```

## Contributing

Contributions are welcome. Please ensure code follows Go best practices and includes tests for new features.

## Roadmap

Features that should be implemented next
- workflow configuration

## License

This project is licensed under the MIT License.