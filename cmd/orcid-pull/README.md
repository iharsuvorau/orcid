**Orcid-pull** downloads scientific publications from [orcid.org](https://orcid.org/) and posts them for each user at MediaWiki who has an ORCID specified on a user profile page.

To quickly deploy to the server, update `Makefile` for your server location, binary and templates destinations, then run (assuming SSH is up and configured):

```
$ make deploy
```

Run it on the server like this:

```
$ orcid-pull -mwuri https://ims.ut.ee/ -name "UserName" -pass "pass" -log "orcid.log"
```
