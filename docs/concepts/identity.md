# Identity & Verification

The Underground Node Network uses a decentralized identity system that links standard SSH keys to established developer platforms.

### Overview
- **Zero Central Passwords**: Identity is established entirely via SSH public key ownership.
- **Platform Linking**: Users verify their keys against public profiles (GitHub, GitLab, Codeberg, etc.).
- **Global Usernames**: Verified users claim a unique UNN username that is protected and recognized across the entire network.

### Registration Process
1. **Verification**: The entrypoint fetches your public keys from a chosen platform (e.g., `github.com/username.keys`).
2. **Matching**: It compares these keys against the one you are currently using to connect.
3. **Naming**: Once matched, you choose a UNN username (4-20 alphanumeric characters).
4. **Persistence**: Your key hash and username are saved to the `users` database.

### Onboarding
For new users, the entrypoint presents a "Registration Form" directly in the terminal before allowing access to the main BBS. This ensures every room-hosting user has a verifiable identity.

---
See also: [Room Authentication](room.md#authentication)
