# Run
Open a Python shell, run a script and run tests

## Python shell
Starts an interactive Python prompt. Ctrl-D leaves it.

```sh
python3
```

## Run a script
Runs a Python file.

```sh
python3 <script>
```

- `<script>` Which script

## Run tests
Runs the project's tests with pytest.

```sh
python3 -m pytest <flags>
```

- `<flags>` Extra options
  - [ ] `--lf` only the ones that failed last time
  - [ ] `-q` quiet
  - [ ] `-x` stop at the first failure
