# 🔥 fyra

`fyra` is a command-line tool for deploying static sites and single-page applications onto [fyra.sh](https://fyra.sh)

Push a directory, get a live URL behind a CDN, no Dockerfiles, no build packs, no config files.

```
$ fyra push
==> Uploading 142 files (3.2 MB)
==> Deployed to https://my-app.apps.fyra.sh
```

## Install

**macOS / Linux / Windows:**

```sh
curl -fsSL https://fyra.sh/install.sh | sh
```

## Documentation

To find out more on how to use the cli, check out our <a href="https://fyra.sh/help.html">documentation</a>.

## Building

**Prerequisites:** Go 1.25+

```sh
git clone https://github.com/fyrash/fyra-cli.git
cd fyra-cli
make build
```

This compiles `fyra` into `bin/fyra`

### Running tests

```sh
make test
```

### Generating protobuf

**Prerequsites:** [Buf.build](https://buf.build/docs/cli/installation/)
```sh
make proto
```


## Bugs

For bugs and issues, please file them in the <a href="https://github.com/fyrash/fyra-cli/issues">issue tracker</a>.

## License

Apache 2.0 License

All rights reserved.
