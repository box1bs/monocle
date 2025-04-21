package logo

import "fmt"

func PrintLogo() {
	fmt.Println(`
Welcome to Saturday!
====================
			.
                      .%
                     |..|
                 ..     .#0         |
  .|0%+||.|          .  .%%.     ||.
     .##%0. ||       . |#%.     .000.
      |+##.   |      . %|     |0##%.
     . .+%#%0.        +.     +###+
       .+#%|.  .|      ||...0%%0|
        .|%###%+++++0+0|0+00+%#%+0.
            .0+++0|#%#######%%%%%+||.
                   +0 .#####0
                   +    .0+++%|
                   0           ..

Saturday: A search robot and engine for students and explorers.

Features:
	- Efficient web crawling with configurable depth and rate limiting
	- Advanced text processing with stemming and stop word filtering
	- Hybrid search ranking (TF-IDF, BM25, machine learning) for precise results
	- Persistent storage with BadgerDB for scalable indexing
	- REST API for remote control and search functionality

Usage of ./saturday:
  	-cli
        Run in CLI mode instead of REST server
  	-config string
        Path to configuration file (default "configs/search_config.json")
  	-log string
        Path to log file (default "logs/indexedURLs.txt")
  	-srv-port int
    	REST server port (default 50051)

Happy exploring!
	`)
}