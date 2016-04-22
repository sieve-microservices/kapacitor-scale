## Usage

### Configuration

kapacitor.toml:

```
[udf]
[udf.functions]
  [udf.functions.scale]
    socket = "/tmp/kapacitor-scale.sock"
    timeout = "10s"
```

then start kapacitor-scale like this:

```
$ kapacitor-scale -
```

### Example

```
stream...
  .scale()
    .id('rancherServiceId')
    .when('value > 10')
    .by('current + 1')
    .min_instances(2)
    .max_instances(10)
    .cooldown('1m')
```

#### Options

- id: Id of the rancher service to scale
- when: expression, should evaluate to true
  (see https://github.com/pk-rawat/gostr)
- by: expression, should evaluate to a number
  (see https://github.com/pk-rawat/gostr)
- max\_instances: maximum instances to scale out
- max\_instances: minimum instances to scale in
- cooldown: timeout to wait until next scaling action

## Tests

$ go test ./handler

## TODO

- start cooldown timer, when service was actually scaled up
- do not freak out, when rancher service is not available
- (free up unneeded services, when removed from rancher)
- procname contains rancher url with credentials! (change argv)
