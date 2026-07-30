# Contributing

Thank you for contributing to Hikarinagi.

This public repository is a one-way source mirror of Hikarinagi's private development monorepo.
Please open issues and pull requests here as usual. Required checks and an approving maintainer
review are the acceptance gate. A maintainer then runs the contribution importer, which maps the
public patch into a private draft pull request without running private CI.

After the imported diff is inspected, a maintainer explicitly activates the private pull request.
That activation runs the normal upstream validation. Once the upstream pull request is merged, the
public mirror is updated and this pull request is closed automatically. Do not merge public mirror
pull requests directly.

Do not include credentials, production data, copyrighted media, or private API responses in issues
or pull requests.
