This tutorial assumes Golang is installed and its binary available in the PATH.

1. First install pkgsite using:
* ```go install golang.org/x/pkgsite/cmd/pkgsite@latest```

---
---

2. Then add /usr/local/go/bin to the PATH:

Using Linux: Add the following line to ~/.profile :
* ```export PATH="/usr/local/go/bin:$PATH"```

then run:
* ```source ~/.profile```

---

Using MacOS: Add the following line to ~/.zshrc :
* ```export PATH="/Users/$USER/go/bin:$PATH"```

then run:
* ```source ~/.zshrc```

---

Using Windows (manually add it to path via environment variables):
* ```/home/$USER/go/bin```  [not sure if $USER is valid on windows, when in doubt just put your username]

---
---

3. Then navigate to the root dir of the repo and run:
* ```pkgsite -open .```

---
---

4. Congrats, now just open ```http://localhost:8080/``` in the browser to view the locally hosted documentation.

---
---

Note: If you are having issues on getting pkgsite to run ensure that your Go installation is correctly configured. When you run
```go env GOPATH```
it should print the path to <yourHomeFolder>/go.
The path to the gophy project should be something like <yourHomeFolder>/go/src/example.org/gophy.

Pkgsite on windows might not work when a different setup is used.
