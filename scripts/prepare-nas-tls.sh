#!/usr/bin/env bash
# Run on the NAS, not inside Dockhand. Never replaces an existing certificate/key.
set -euo pipefail
if [ "$#" -ne 1 ] || [[ "$1" != /* ]]; then
  echo 'Usage: sudo bash scripts/prepare-nas-tls.sh /absolute/NAS/tls-directory' >&2
  exit 1
fi
if [ "$(id -u)" -ne 0 ] && [ "$(id -u)" -ne 1000 ]; then
  echo 'Run with sudo/root (or as UID 1000) to assign the new private key to container UID 1000.' >&2
  exit 1
fi
tls_dir=$1
if [ ! -d "$tls_dir" ]; then
  mkdir -p "$tls_dir"
  chmod 755 "$tls_dir"
fi
if [ -e "$tls_dir/cert.pem" ] || [ -e "$tls_dir/key.pem" ]; then
  test -r "$tls_dir/cert.pem" && test -r "$tls_dir/key.pem"
  openssl x509 -in "$tls_dir/cert.pem" -checkend 0 -noout
  cert_public=$(openssl x509 -in "$tls_dir/cert.pem" -pubkey -noout | openssl pkey -pubin -outform DER | openssl dgst -sha256)
  key_public=$(openssl pkey -in "$tls_dir/key.pem" -pubout -outform DER | openssl dgst -sha256)
  test "$cert_public" = "$key_public"
  echo 'Existing matching certificate/key preserved. Verify SANs and UID-1000 read access before startup.'
  exit 0
fi
umask 077
stage_dir=$(mktemp -d "$tls_dir/.new-tls.XXXXXX")
trap 'rm -rf "$stage_dir"' EXIT
openssl req -x509 -newkey rsa:3072 -nodes -days 365 \
  -keyout "$stage_dir/key.pem" -out "$stage_dir/cert.pem" \
  -subj /CN=drk-nas.internal \
  -addext 'subjectAltName=DNS:drk-nas.internal,DNS:localhost,IP:127.0.0.1' \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,digitalSignature,keyEncipherment,keyCertSign' \
  -addext 'extendedKeyUsage=serverAuth'
chown 1000:1000 "$stage_dir/key.pem" "$stage_dir/cert.pem"
chmod 600 "$stage_dir/key.pem"
chmod 644 "$stage_dir/cert.pem"
# Hard links fail on existing destinations, protecting concurrent/pre-existing files.
ln "$stage_dir/key.pem" "$tls_dir/key.pem"
ln "$stage_dir/cert.pem" "$tls_dir/cert.pem"
echo 'Created backend certificate and UID-1000 private key. Copy only cert.pem to Traefik; validity is 365 days.'
