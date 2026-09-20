# internal/i18n

`main_en_US.properties`, `main_de_DE.properties`, `main_de_AT.properties`
and `main_de_CH.properties` are BlogCFC's own resource bundles
(`client/includes/`, BlogCFC 5.9.8 by Raymond Camden, Apache-2.0), copied
verbatim so the rewrite's strings read exactly as the as-is app's do; the
files carry no comment of their own because a comment would change them.
The German three are byte-identical in the as-is, and stay separate here
for the same reason BlogCFC kept them: they are different locales, and
their dates differ (`dates.go`).

`bundle.go` reads them as Java `.properties` files and falls back to
en_US for an unknown locale and for a key a bundle is missing - the
German files are missing four of en_US's keys (PLAN §9 R05).

`dates.go` holds the month and day names, the week start and the date
formats for those four locales (PLAN §9 R06). BlogCFC asked the JVM for
all of it through `org/hastings/locale/utils.cfc`; the tables reproduce
what that JVM answered, down to the JRE's pre-CLDR German ("Mrz", and
"Jänner" in de_AT).
