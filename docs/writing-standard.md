# Writing Standard

TreeMan documentation uses ASD-STE100 Simplified Technical English, Issue 9, as its writing standard.

The standard has writing rules and a controlled dictionary. TreeMan does not have a tool that validates the full dictionary. Therefore, this project does not claim formal ASD-STE100 certification.

Read the [official ASD-STE100 Issue 9 document](https://www.asd-ste100.org/assets/files/ASD-STE100_ISSUE9.pdf).

## Required Rules

- Use approved ASD-STE100 words where possible.
- Use American English.
- Use one word with one meaning.
- Use active voice where possible.
- Use simple verb tenses.
- Use imperative verbs for procedures.
- Put one instruction in each procedure step.
- Put conditions before their related instruction.
- Keep procedural sentences to 20 words or fewer.
- Keep descriptive sentences to 25 words or fewer.
- Do not use unnecessary synonyms, jargon, or ambiguous `-ing` forms.
- Use lists when a sentence would contain many actions or conditions.
- Define and use terms from [Terminology](terminology.md).

## Technical Terms

TreeMan technical nouns and technical verbs can use words outside the ASD-STE100 dictionary. These terms include `worktree`, `GitLab`, `fzf`, `TOML`, and `PostgreSQL`.

Use a technical term only with its defined meaning. Add a term to [Terminology](terminology.md) before you use a new term in general prose.

## Literal Content

Do not change these items to meet language rules:

- Commands, flags, and command output
- Code, configuration, and environment variables
- File paths, URLs, API paths, package names, and Git references
- Product names, trademarks, and technical names

Explain literal content with controlled prose.

## Review Checklist

Before merge, check each documentation change.

1. Identify procedure text and descriptive text.
2. Check sentence word count.
3. Check that each procedure step has one action.
4. Check active voice and simple tense.
5. Check terms against [Terminology](terminology.md).
6. Check commands and behavior against source code.
7. Check internal links.
8. Add a limitation when the code behavior is unsafe or incomplete.
