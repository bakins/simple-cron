# simple-cron

`simple-cron` is a simple job scheduler that launches a single procees
on a cron-like schedule.

## Installation

Binary releases are availible for [download](https://github.com/bakins/simple-cron/releases)

`simple-cron` is also go gettable. `go get -u github.com/bakins/simple-cron`
will install the version in master.

## Usage

```shell
$ simple-cron --help
single process job scheduler

Usage:
  simple-cron [flags]

Flags:
  -a, --address string    address for HTTP listener for metrics (default ":2766")
  -h, --help              help for simple-cron
  -n, --name string       name of job for metrics. If unset, the command is used
  -s, --schedule string   cron expression of desired schedule. (default "15 * * * *")
```

Example:

```shell
$ simple-cron -s "* * * * *" sleep 70
```

Will run `sleep 70` once a minute. Note: simple-cron will not start more than
one instance of the job.

`simple-cron` can also use environment variables for configuration.
These are in the form of `CRON_` and the uppercase name of the flag.  For example,
setting the environment variable `CRON_SCHEDULE="* * * * *"` is the same
as passing a flag like `--schedule="* * * * *"`


To run a command that takes command line flags, use `--` to seperate flags to `simple-cron` from you program:

```shell
$ simple-cron -s "* * * * *" --name=bar -- ls -al
```

## TODO

- flag to allow concurrent running jobs?
- expose statsd server for processes?

## License

See [LICENSE](./LICENSE)
