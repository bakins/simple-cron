# simple-cron

`simple-cron` is a simple job scheduler that launches a single procees
on a cron-like schedule.

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

## TODO

- flag to allow concurrent running jobs?
- expose statsd server for processes?

## License

See [LICENSE](./LICENSE)
