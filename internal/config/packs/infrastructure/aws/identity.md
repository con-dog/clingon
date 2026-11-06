# Identity
Switch AWS profiles, see who you are, and log in and out of SSO

## Use profile
Sets which AWS account and credentials this shell uses for every aws command after it.

```sh
export AWS_PROFILE=<profile>
```

- `<profile>` Which profile

## Stop using a profile
Goes back to your default AWS profile in this shell.

```sh
unset AWS_PROFILE
```

## Who am I
Shows which AWS account, user or role your current credentials belong to.

```sh
aws sts get-caller-identity
```

## Who am I as a profile
Shows which AWS account, user or role a profile's credentials belong to, without switching to it.

```sh
aws sts get-caller-identity --profile <profile>
```

- `<profile>` Which profile

## List profiles
Lists the name of every profile in your AWS config, SSO ones included.

```sh
aws configure list-profiles
```

## SSO login
Signs you in through AWS SSO in your browser and caches fresh credentials.

```sh
aws sso login --profile <profile>
```

- `<profile>` Which profile

## SSO logout
> [!WARNING]
> Signs you out of AWS SSO by clearing its cached sign-ins for every profile, not just the current one.

```sh
aws sso logout
```
