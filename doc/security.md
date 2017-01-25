# Upspin Security

## Introduction

The security design of Upspin has been sketched in the accompanying
[Upspin overview](/doc/overview.md) document.
Here we dive into the deeper security issues.
Some of the discussion may be of interest only to experts, but the general
design should be understood by anyone given background provided in the
referenced links.

When running the Directory and Storage servers on public cloud infrastructure
such as Google Cloud, Upspin attempts to provide:

1. confidentiality and integrity protection of content even against state
   actors, and
1. protection of metadata against moderately capable attackers, but not against
   due legal process.
   Especially cautious users can run their directory server in a private cloud
   to protect themselves against intrusion via metadata search warrants.

Upspin's security model assumes that the Client endpoint platform is secure,
that the Directory service is run on a machine considered adequately secure by
its shared users, and that the Store service is reasonably available but not
necessarily trusted for confidentiality.

## Upspin-specific Storage

It is feasible to use Upspin with existing, perhaps unencrypted, storage
systems.

But Upspin's design can securely build on top of untrusted public cloud
storage, and that will be the primary emphasis in this document.

In our design, Alice (which is to say, Upspin software run by Alice) shares a
file with Bob by picking a new random
[symmetric key](https://en.wikipedia.org/wiki/Symmetric-key_algorithm),
encrypting a file, wrapping the symmetric encryption key with Bob's
[public key](https://en.wikipedia.org/wiki/Public-key_cryptography),
[signing](https://en.wikipedia.org/wiki/Digital_signature) the file using her
own [elliptic curve](https://en.wikipedia.org/wiki/Elliptic_curve_cryptography)
private key, and sending the ciphertext to a storage server and metadata to a
directory server.

The specific ciphersuite used is selectable (choices are plain text, meaning no
encryption, and elliptic-curve; more may arise) and defaults to P-256 for the
elliptic curve algorithm, AES-256 for data encryption, and SHA-256 for
checksums.
The entire system is written in Go, is open-source, and uses Go's standard
cryptographic packages.

The basic idea is to choose a random number as an encryption key K, use AES to
encrypt the data, and store the encrypted data in the storage server.
Then, we encrypt K again, repeatedly using the private key of each potential
reader of the file.
We store those encrypted keys in the `DirEntry` for the item along with a
digital signature of the data.
To read the data, the reader looks in the `DirEntry` for the reader's
encryption of K, decrypts K, and uses that to decrypt the data.

The next few paragraphs explain this process in detail for security experts and
can be skipped by less dedicated readers.

To store a file "*pathname"*, Alice obtains a fresh 256 bit random "*dkey*" and
XORs the file with an AES-CTR bitstream with IV=0.
The ciphertext is sent to the Store server.
The Store server returns a cryptographic location string, called a reference,
that we assume may safely be given to anyone and used to retrieve the
ciphertext.
Thus Alice leaks information to the Store server consisting of the creation
time and the file size, but nothing else.

A username list {U} is assembled including Bob, Alice, and any others granted
read access to items in the pathname's directory.
Alice looks up each of the username's public key P(U) from (a local cache of) a
centralized `KeyServer` running at `key.upspin.io`.
Alice wraps *dkey* for each reader, annotated with a hash of that user's public
key.
(Alice runs with only ciphersuites she considers adequate, say
{p256,p384,p521}, though she may herself use a p384 key.
If Bob picks an RSA 1024 key, she'll decline to wrap for him.)

Keys are wrapped as in NIST 800-56A rev2 and RFC6637 §8 using ECDH.
TODO LINKS.
Specifically, Alice creates an ephemeral key pair v, V=vG based on the agreed
elliptic curve point G and random v.
Using Bob's public key R, Alice computes the shared point S = vR.
A shared secret "*strong*" is constructed by HKDF of S and a string composed of
the ciphersuite, the hash of Bob's public key, and a nonce.
Next, *dkey* is encrypted by AES-GCM using the key *strong*.
This yields a wrapping

```
W(dkey,U) = {sha256(P(U)), nonce, V, aes(dkey,strong)}
```

which Bob can unwrap by looking through the list for his public key hash, then
computing S = rV using his private key r, then reconstructing the strong shared
secret via HKDF, and finally AES-GCM decrypting to recover dkey.

Using her private key, Alice signs

```
sha256("*ciphersuite*:*pathname*:*time*:*dkey*:*ciphertext*")
```

By signing, Alice ensures that even a reader colluding with upspin servers
cannot change the file contents undetected.
Alice is only claiming that she intended to save those contents with that
pathname, not that she necessarily is the original author or even that the
contents are harmless; in this regard, we're adopting the same semantics as
"owner" in a classic Unix filesystem.

We do not insist that Alice bind her name inside the file contents.
It is cryptographically possible that two authors of a file could each have
their own equally valid directory entries pointing to the same storage blob.
However, unlike with some content-addressable storage systems, if two
individuals write the same cleartext, it will almost certainly be encrypted
with different keys and thus be stored twice, once for each encryption.

The list of readers for key wrapping is taken from the read access list
described in the [Access Control](/doc/access_control.md) document.
When that list changes, wrapped keys should be removed for the dropped readers
and extra wrapped keys made for the added readers.
The Directory server manages this work queue, but needs cooperation of the
owner's Client to do the actual wrapping for new readers.
This lazy update process can also handle readers' public keys changing over
time, which helps users who have lost old keys.
It is inherent in the notion of a file archive that there is no perfect forward
secrecy.
However, a somewhat similar effect is achieved by this update process.

The pathname, revision number, encrypted content location, signature, and
wrapped keys are the primary metadata about a file stored by the Directory
server.
Thus Alice leaks information to the Directory server, particularly the
cleartext pathnames and the (public keys of the) people she is sharing with.
Also, to the extent that elliptic curves might be cryptographically weaker over
time than AES, Alice also depends on the Directory server being unwilling to
distribute data to unauthorized people.

The random bit generation, file encryption, and signing/key-wrapping all are
done on the Client, not on any of the servers.
So, with the exception of specific leaks called out above, we intend that this
system provides end-to-end encryption verifiably under the exclusive control of
the end users.

The Directory server needs to store its hierarchy of directory entries
somewhere.
(It is represented as a
[Merkle tree](https://en.wikipedia.org/wiki/Merkle_tree), a tree of hash
values.)
The server could use the encryption scheme described above and store its data in
the storage server the same way a user does, but that method is cumbersome and
cryptographically unnecessary since no sharing is involved.
We instead use a simple symmetric key known only to the Directory server and
AEAD, specifically AES GCM.

## Key Management

An Upspin user joins the system by publishing a key to a central key server.
We're running our own server for the moment but anticipate sharing with a
CONIKS-like system being built for e2email.
[TODO: link to blog post]

As far as Upspin is concerned, a user is an email address, authenticated by an
elliptic curve key pair used for signing and encrypting.
We anticipate that the user will rotate keys over time, but we also assume that
they will retain all old key pairs for use in decrypting old content, and will
accept losing that access to that content if they lose all copies of their keys.

To generate a new key pair, a user executes `keygen` and copies on paper the
128 bit seed as backup.
This seed is, expressed as a proquint
[[arxiv.org/html/0901.4016](https://arxiv.org/html/0901.4016)].
The keygen program saves the elliptic curve public and private keys, as decimal
integers in plain text files in the user's home .ssh directory.
A user should be able to "restore" keys to multiple devices including
smartphones.

The public part of the key pair is stored in a file `public.upspinkey,`
conventionally in the directory $HOME/.ssh/ along with the user's other keys.
The SHA-256 hash of that file is called the `keyHash` and is used to identify
which readers have cryptographic access to data contents via encryption key
wrapping.
This file can safely be given to anyone, and is the material registered at the
key server.
The private part of the key pair is stored in a file `secret.upspinkey,` also
in ~/.ssh/, and is read-protected to the user by normal file permissions (but
no extra passphrase).
Eventually, we envision that such secrets will be protected by hardware but
we're starting with local file as more portable for initial deployment.
If you want some amount of hardware protection, use an encrypted filesystem or
Ironkey for ~/.ssh.
Older key pairs, both public and private parts, are stored in a file
`secret2.upspinkey`.
Based on past experience with PGP, our choice of filenames is intended to help
the average user avoid the common mistake of confusing which information can be
freely shared and which needs to be carefully protected.
Key rotation happens in the following sequence of operations:

<table>
  <tr>
    <td>upspin cmd operation</td>
    <td><code>public,secret.upspinkey</code></td>
    <td><code>secret2.upspinkey</code></td>
    <td>keyserver</td>
    <td>signatures</td>
    <td>wraps</td>
  </tr>
  <tr>
    <td>initial key</td>
    <td>k1</td>
    <td>-</td>
    <td>k1</td>
    <td>k1, -</td>
    <td>k1</td>
  </tr>
  <tr>
    <td>new key</td>
    <td>k2</td>
    <td>k1</td>
    <td>k1</td>
    <td>k1, -</td>
    <td>k1</td>
  </tr>
  <tr>
    <td>countersign</td>
    <td>k2</td>
    <td>k1</td>
    <td>k1</td>
    <td>k2, k1</td>
    <td>k1</td>
  </tr>
  <tr>
    <td>rotate</td>
    <td>k2</td>
    <td>k1</td>
    <td>k2</td>
    <td>k2, k1</td>
    <td>k1</td>
  </tr>
  <tr>
    <td>share -fix</td>
    <td>k2</td>
    <td>k1</td>
    <td>k2</td>
    <td>k2, k1</td>
    <td>k2</td>
  </tr>
</table>

We do not anticipate that the keys used here will be used for any other
purpose, and we've chosen proquint as an obscure technology to promote that
independence.
We therefore do not think there are any viable protocol interleaving attacks.

With `secret.upspinkey,`we follow Chrome's password-manager reasoning that if
the user does not have encrypted disk storage or is not in exclusive control of
their home directory, they have lost the security game anyway and there is
nothing meaningful we can do to protect them.
As with Chrome, we realize this will be a controversial position.
We look forward to adopting some Security Key or other hardware-protected
private key storage.
There are no passwords in our system and we don't intend to have any.

By collecting all the private key operations into the factotum package, we are
providing for an isolated implementation, as in qubes-split-gpg or ssh-agent.

## Server Management

We're currently running our Store server (for encrypted bulk file content),
Directory server (for metadata), and User server (for keys and location of
directory server) on GKE at domain name `upspin.io`.

A user connects to these servers by HTTPS, implicitly using TLS 1.2.
To identify the user accessing any Upspin server, the RPC framework presents an
authentication request signed with the user's private key.
This protocol guarantees that only registered Upspin users can access Upspin
services.
(Reads from the Key server do not require authentication.)

Administrators of Store and Directory servers can use the authenticated user
name to restrict write access to a subset of all Upspin users.
For instance, an instance of the default StoreServer can maintain a list of
users permitted to store blocks on the server.

The `upspin.io` servers use certificates from LetsEncrypt.
You may use the default system Root CA list, or specify `tlscerts` in your
`~/upspin/rc` pointing to a directory with just `DST_Root_CA_X3.pem`.
