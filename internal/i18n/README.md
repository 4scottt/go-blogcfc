# internal/i18n

`main_en_US.properties` is BlogCFC's own resource bundle
(`client/includes/main_en_US.properties`, BlogCFC 5.9.8 by Raymond Camden,
Apache-2.0), copied verbatim so the rewrite's strings read exactly as the
as-is app's do; the file carries no comment of its own because a comment
would change it.

`bundle.go` reads it as a Java `.properties` file (PLAN §9 R05). Only
`en_US` exists today; `de_DE` arrives with M2.
