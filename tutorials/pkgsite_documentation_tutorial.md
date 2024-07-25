This tutorial assumes Golang is installed and its binary available in the PATH.

1. First install pkgsite using:
* ```go install golang.org/x/pkgsite/cmd/pkgsite@latest```

---

2. Then add /usr/local/go/bin to the PATH:

Using Linux: Add the following line to ~/.profile :
* ```export PATH="/usr/local/go/bin:$PATH"```

Using MacOS: Add the following line to ~/.zshrc :
* ```export PATH="/Users/$USER/go/bin:$PATH"```
    then on MacOS you run:
* ```source ~/.zshrc```
   	or on Linux you run:
* ```source ~/.profile```

Using Windows (manually add it to path via environment variables):
* ```/home/$USER/go/bin```  [not sure if $USER is valid on windows, when in doubt just put your username]

---

3. Then run:
* ```pkgsite -open .```

---

4. Congrats, now just open ```http://localhost:8080/``` in the browser to view the locally hosted documentation.
