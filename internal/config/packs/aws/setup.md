# Setup
See your current AWS settings, set a default region, and set up access keys or SSO

## Current settings
Shows which profile, credentials and region aws commands use right now, and where each comes from.

```sh
aws configure list
```

## Set default region
Sets the region a profile uses when a command doesn't say.

```sh
aws configure set region <region> --profile <profile>
```

- `<profile>` Which profile
- `<region>` Which region
  - ( ) `ap-northeast-1`
  - ( ) `ap-south-1`
  - ( ) `ap-southeast-1`
  - ( ) `ap-southeast-2`
  - ( ) `ca-central-1`
  - ( ) `eu-central-1`
  - ( ) `eu-north-1`
  - ( ) `eu-west-1`
  - ( ) `eu-west-2`
  - ( ) `eu-west-3`
  - ( ) `sa-east-1`
  - (x) `us-east-1`
  - ( ) `us-east-2`
  - ( ) `us-west-1`
  - ( ) `us-west-2`

## Set up access keys
> [!WARNING]
> Saves an access key, secret and default region for a profile, asking for each.

```sh
aws configure --profile <profile>
```

- `<profile>` Which profile
  - (x) `default`

## Set up SSO
Creates a profile that signs in through AWS IAM Identity Center (SSO), asking a few questions first.

```sh
aws configure sso
```
