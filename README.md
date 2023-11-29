<p align="center">
  <a href="https://nitric.io">
    <img src="docs/assets/nitric-logo.svg" width="120" alt="Nitric Logo"/>
  </a>
</p>

<p align="center">
  CLI for building and deploying <a href="https://nitric.io">nitric</a> apps
</p>

<p align="center">
  <img alt="GitHub Workflow Status (branch)" src="https://img.shields.io/github/actions/workflow/status/nitrictech/cli/cli-test.yaml?style=for-the-badge&branch=develop">
  <img alt="GitHub release (latest SemVer)" src="https://img.shields.io/github/v/release/nitrictech/cli?style=for-the-badge">
  <img alt="GitHub all releases downloads" src="https://img.shields.io/github/downloads/nitrictech/cli/total?style=for-the-badge">
  <a href="https://discord.gg/Webemece5C"><img alt="Discord" src="https://img.shields.io/discord/955259353043173427?label=discord&style=for-the-badge"></a>
</p>

## Overview

Nitric provides a command-line interface (CLI) to assist with various tasks when building applications with the Nitric framework. For more information, check out the main [Nitric repo](https://github.com/nitrictech/nitric).

## Installation

The Nitric CLI is free to [download and install](https://nitric.io/docs/installation).

## Purpose

The Nitric CLI performs 3 main tasks:

- Create new projects
- Run apps locally for testing and development
- Deploy to the cloud you choose

## Common Commands

Common commands in the CLI that you’ll be using:

- nitric down : Undeploy a previously deployed stack, deleting resources
- nitric preview : Preview the deployment of a stack
- nitric run : Run your project locally for development and testing
- nitric stack new [stackName] [providerName] : Create a new Nitric stack
- nitric start : Run nitric services locally for development and testing
- nitric up : Create or update a deployed stack

## Help with Commands

Each command is self documented and provides a "help" interface describing the usage, arguments and options for the command. Use the help command to view the help information for any other command:

Example displaying help for the `new` command

```bash
nitric new --help
```

## Complete Reference

Documentation for all available commands:

- nitric feedback : Provide feedback on your experience with nitric
- nitric info : Gather information about Nitric and the environment
- nitric new [projectName] [templateName] : Create a new project
- nitric run : Run your project locally for development and testing
- nitric stack : Manage stacks (the deployed app containing multiple resources e.g. collection, bucket, topic)
- nitric stack down [-s stack] : Undeploy a previously deployed stack, deleting resources
  (alias: nitric down)
- nitric stack new [stackName] [providerName] : Create a new Nitric stack
- nitric stack preview [-s stack] : Preview the deployment of a stack
  (alias: nitric preview)
- nitric stack update [-s stack] : Create or update a deployed stack
  (alias: nitric up)
- nitric start : Run nitric services locally for development and testing
- nitric version : Print the version number of this CLI

## Get in touch

- Ask questions in [GitHub discussions](https://github.com/nitrictech/nitric/discussions)

- Join us on [Discord](https://discord.gg/Webemece5C)

- Find us on [Twitter](https://twitter.com/nitric_io)

- Send us an [email](mailto:maintainers@nitric.io)

## Get Started

Check out the [Nitric docs](https://nitric.io/docs) to see how to get started using Nitric.
