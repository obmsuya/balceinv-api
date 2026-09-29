#!/usr/bin/env bash
set -euo pipefail
umask 077

stack_directory=/opt/balce
env_file="$stack_directory/.env.prod"
example_file="$stack_directory/.env.prod.example"
generated_keys=()

touch "$env_file"
chmod 600 "$env_file"

while IFS= read -r example_line; do
  key_name=${example_line%%=*}
  if [[ -z "$key_name" || "$key_name" == \#* ]]; then
    continue
  fi
  case "$key_name" in
    S3_ACCESS_KEY_ID|S3_SECRET_ACCESS_KEY) continue ;;
  esac

  current_value=$(grep -E "^${key_name}=" "$env_file" | head -1 | cut -d= -f2- || true)
  if [[ -n "$current_value" ]]; then
    continue
  fi

  sed -i "/^${key_name}=/d" "$env_file"
  printf '%s=%s\n' "$key_name" "$(openssl rand -hex 32)" >> "$env_file"
  generated_keys+=("$key_name")
done < "$example_file"

echo "generated: ${generated_keys[*]:-nothing, all keys already set}"
