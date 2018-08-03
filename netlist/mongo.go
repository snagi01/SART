package netlist

import (
    "log"
    "sync"

    "gopkg.in/mgo.v2"
    "gopkg.in/mgo.v2/bson"
)

var mgosession *mgo.Session

const db = "sart"

var nodecoll, linkcoll, snetcoll string

////////////////////////////////////////////////////////////////////////////////
// Worker pool for insert jobs

const MaxMgoThreads = 8

var wg sync.WaitGroup

type insertjob struct {
    col string
    doc interface{}
}

var jobs chan insertjob

func worker() {
    s := mgosession.Copy()

    for job := range jobs {
        c := s.DB(db).C(job.col)
        err := c.Insert(job.doc)
        if err != nil {
            log.Fatal(err)
        }
    }
    wg.Done()
}

// Synchronizers

func DoneMgo() {
    close(jobs)
}

func WaitMgo() {
    wg.Wait()
}

////////////////////////////////////////////////////////////////////////////////

func InitMgo(s *mgo.Session, cname string, drop bool) {
    mgosession = s.Copy()

    nodecoll = cname + "_nnodes"
    linkcoll = cname + "_nlinks"
    snetcoll = cname + "_nsnets"

    var err error

    if drop {
        dropCollection(nodecoll)
        dropCollection(linkcoll)
        dropCollection(snetcoll)
    }

    n := mgosession.DB(db).C(nodecoll)
    err = n.EnsureIndex(mgo.Index{ Key: []string{"module", "name"}, Unique: true })
    if err != nil { log.Fatal(err) }

    l := mgosession.DB(db).C(linkcoll)
    err = l.EnsureIndex(mgo.Index{ Key: []string{"module"} })
    if err != nil { log.Fatal(err) }

    b := mgosession.DB(db).C(snetcoll)
    err = b.EnsureIndex(mgo.Index{ Key: []string{"module", "name"}, Unique: true })
    if err != nil { log.Fatal(err) }

    // Initialize worker pool for insert jobs
    jobs = make(chan insertjob, 100)
    for i := 0; i < MaxMgoThreads; i++ {
        wg.Add(1)
        go worker()
    }
}

func dropCollection(coll string) {
    c := mgosession.DB(db).C(coll)
    err := c.DropCollection()
    if err != nil {
        log.Println(err)
    }
}

func (n *Netlist) Save() {
    for _, node := range n.Nodes {
        jobs <- insertjob{nodecoll, node}
    }

    // Links is a map of right-nodes indexed using the fullname of the
    // left-node. It is sufficient to push just the fullname of the rnode into
    // mongo as during retrieval, the right-node-fullname can be used to locate
    // the node which should already have been loaded.
    for lfullname, rnodes := range n.Links {
        for _, rnode := range rnodes {
            doc := bson.M{
                "module": n.Name,
                "lfullname": lfullname,
                "rfullname": rnode.Fullname(),
            }
            jobs <- insertjob{linkcoll, doc}
        }
    }

    for _, subnet := range n.Subnets {
        doc := bson.M{
            "module": n.Name,
            "name"  : subnet.Name,
        }
        jobs <- insertjob{snetcoll, doc}
    }
}

func (n *Netlist) Load() {
    var result bson.M

    // nodes collection, query and iterator
    nc := mgosession.DB(db).C(nodecoll)
    nq := nc.Find(bson.M{"module": n.Name})
    ni := nq.Iter()

    for ni.Next(&result) {
        bytes, err := bson.Marshal(result)
        if err !=nil {
            log.Fatalf("Unable to marshal. module:%q name:%q err:%v",
                       result["module"], result["name"], err)
        }

        var node Node
        err = bson.Unmarshal(bytes, &node)
        if err != nil {
            log.Fatalf("Unable to umarshal. module:%q name:%q err:%v",
                       result["module"], result["name"], err)
        }

        n.AddNode(&node)
    }

    // subnet collection, query and iterator
    sc := mgosession.DB(db).C(snetcoll)
    sq := sc.Find(bson.M{"module": n.Name}).Select(bson.M{"_id":0, "module":0})
    si := sq.Iter()

    for si.Next(&result) {
        fullname := result["name"].(string)
        subnet := NewNetlist(fullname)
        n.Subnets[fullname] = subnet
        subnet.Load()
    }

    // link collection, query and iterator
    lc := mgosession.DB(db).C(linkcoll)
    lq := lc.Find(bson.M{"module": n.Name}).Select(bson.M{"_id":0})
    li := lq.Iter()

    for li.Next(&result) {
        lfullname := result["lfullname"].(string)
        rfullname := result["rfullname"].(string)
        lnode := n.LocateNode(lfullname)
        rnode := n.LocateNode(rfullname)

        if lnode == nil {
            log.Fatalf("Could not locate lnode %q in netlist %q", lfullname, n.Name)
        }

        if rnode == nil {
            log.Fatalf("Could not locate rnode %q in netlist %q", rfullname, n.Name)
        }

        n.Connect(lnode, rnode)
    }
}
