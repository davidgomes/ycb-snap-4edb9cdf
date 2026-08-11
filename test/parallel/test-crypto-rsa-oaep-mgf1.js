'use strict';
const common = require('../common');
if (!common.hasCrypto)
  common.skip('missing crypto');

// Verify that crypto.publicEncrypt / crypto.privateDecrypt can select the
// RSA-OAEP MGF1 digest independently via `mgf1Hash`. Omitting it keeps MGF1
// tied to `oaepHash` (default sha1).

const assert = require('assert');
const crypto = require('crypto');
const fixtures = require('../common/fixtures');
const { hasFIPS } = require('../common/crypto');

const rsaPubPem = fixtures.readKey('rsa_public.pem', 'ascii');
const rsaKeyPem = fixtures.readKey('rsa_private.pem', 'ascii');
const rsaPkcs8KeyPem = fixtures.readKey('rsa_private_pkcs8.pem');

const plaintext = Buffer.from('I AM THE WALRUS');

const fips3 = hasFIPS(3);
const fips35 = hasFIPS(3, 5);
const oaepDecodingError = fips35 ? {
  code: 'ERR_OSSL_EVP_PROVIDER_ASYM_CIPHER_FAILURE',
} : fips3 ? {
  message: 'error:00000000:lib(0)::reason(0)',
} : {
  code: 'ERR_OSSL_RSA_OAEP_DECODING_ERROR',
};

function encrypt(options) {
  return crypto.publicEncrypt({ key: rsaPubPem, ...options }, plaintext);
}

function decrypt(ciphertext, options) {
  return crypto.privateDecrypt({ key: rsaKeyPem, ...options }, ciphertext);
}

// Agreeing peers with OAEP digest != MGF1 digest round-trip on encrypt
// and decrypt.
{
  const options = { oaepHash: 'sha256', mgf1Hash: 'sha512' };
  const ciphertext = encrypt(options);
  assert.deepStrictEqual(decrypt(ciphertext, options), plaintext);

  const pkcs8Plaintext = crypto.privateDecrypt({
    key: rsaPkcs8KeyPem,
    ...options,
  }, ciphertext);
  assert.deepStrictEqual(pkcs8Plaintext, plaintext);
}

// Omitting mgf1Hash keeps MGF1 following oaepHash, including the default.
{
  const ciphertext = encrypt({ oaepHash: 'sha256' });
  assert.deepStrictEqual(
    decrypt(ciphertext, { oaepHash: 'sha256' }),
    plaintext);
  assert.deepStrictEqual(
    decrypt(ciphertext, { oaepHash: 'sha256', mgf1Hash: 'sha256' }),
    plaintext);
}

// XML Encryption rsa-oaep-mgf1p: OAEP SHA-256 with MGF1 fixed to SHA-1.
if (!fips3) {
  const options = { oaepHash: 'sha256', mgf1Hash: 'sha1' };
  const ciphertext = encrypt(options);
  assert.deepStrictEqual(decrypt(ciphertext, options), plaintext);

  // Explicit mgf1Hash sha1 with default oaepHash matches omitting mgf1Hash.
  const defaultCiphertext = encrypt({ mgf1Hash: 'sha1' });
  assert.deepStrictEqual(
    decrypt(defaultCiphertext, { mgf1Hash: 'sha1' }),
    plaintext);
  assert.deepStrictEqual(decrypt(defaultCiphertext, {}), plaintext);
}

// Ciphertext produced with a distinct MGF1 digest does not decrypt when the
// other side leaves MGF1 defaulted to oaepHash.
{
  const ciphertext = encrypt({ oaepHash: 'sha256', mgf1Hash: 'sha512' });
  assert.throws(() => {
    decrypt(ciphertext, { oaepHash: 'sha256' });
  }, oaepDecodingError);
  assert.throws(() => {
    decrypt(ciphertext, { oaepHash: 'sha256', mgf1Hash: 'sha256' });
  }, oaepDecodingError);
  assert.throws(() => {
    decrypt(encrypt({ oaepHash: 'sha256' }), {
      oaepHash: 'sha256',
      mgf1Hash: 'sha512',
    });
  }, oaepDecodingError);
}

// Invalid mgf1Hash types and unknown digest names fail like oaepHash.
for (const fn of [crypto.publicEncrypt, crypto.privateDecrypt]) {
  assert.throws(() => {
    fn({
      key: rsaPubPem,
      mgf1Hash: 'Hello world',
    }, Buffer.alloc(10));
  }, {
    code: 'ERR_OSSL_EVP_INVALID_DIGEST',
  });

  for (const mgf1Hash of [0, false, null, Symbol(), () => {}]) {
    assert.throws(() => {
      fn({
        key: rsaPubPem,
        mgf1Hash,
      }, Buffer.alloc(10));
    }, {
      code: 'ERR_INVALID_ARG_TYPE',
    });
  }
}
