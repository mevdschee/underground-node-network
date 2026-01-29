# Identity Verification and Storage

The Underground Node Network uses a decentralized identity verification system to establish trust without a central authority.

## How it works

1. **Platform Selection**: You choose a platform (GitHub, GitLab, etc.) to verify your identity.
2. **Key Matching**: The entry point fetches your public keys from the selected platform and compares them to your current SSH session key.
3. **UNN Username Choice**: Once verified, you choose a global UNN username. This name must be unique across the network.
4. **Unified Storage**: Your identity, platform info, and UNN username are stored together in a single `users` file.

Once verified, the entry point will **always** identify you by your registered UNN username, even if you connect with a different SSH username (e.g., `ssh -p 44322 wrongname@entrypoint`).

For first-time users, the registration questions are asked in a plain text interface before the full SSH TUI is initialized.

## Storage Format

All verified users are stored in a single `users` file in the users directory (default: `~/.unn/users/users`).

### In-Memory Management
The entry point reads this entire file at startup and maintains:
- **Hash -> Metadata**: Maps your public key hash to your UNN username and platform identity.
- **Username -> Hash**: Maps UNN usernames to public key hashes to ensure uniqueness.

### File Content
Each line in the `users` file follows the format:
`[pubkey_hash] [unn_username] [platform_username]@[platform]`

**Example:**
`42450b2fe382... maurits mevdschee@github`


## Persistence

The `users` file is saved atomically whenever a new user registers or an existing user changes their name. This ensures data integrity even in the event of a crash.
