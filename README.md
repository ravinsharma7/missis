# Three core parts to "missis"
- the core specs
- the core test suite
- the core reference implementation

# Introduction
- missis has only 3 commands. it is purposely simple in the interface, but the implementation is aggressively complicated. This allows any system integration without complicated interfaces.
- You can hack and build your own "missis" implementation by reusing and mixing any parts of these. it is organized in this way to make it easy to cleanroom port into another language, or extend or change parts of it for different purposes. These three are always stable and constantly will be made more rigorous so anything project extending from this will always have a strong baseline.
- All three implementation will expose some flaws in each other and can be used as basis to continuously improve each other.
- This project uses AI heavily to experiment with matters.