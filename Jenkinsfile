#!/usr/bin/groovy
@Library('jenkins-pipeline-shared@master') _

pipeline {
    agent {
        label "kne"
    }
    stages {
        stage("Build") {
            steps{
                sh "make install"
            }
        }
        stage("Unit Tests") {
            steps {
                sh "make test"
            }
        }
        stage("Integration Tests") {
            steps {
                sh "fkne deploy ./deploy/kne/kind-bridge.yaml"
                sh "fkne create ./examples/openconfig/lemming.pb.txt"
                sh "kubectl get pods -n lemming-twodut"
                sh "fkne delete ./examples/openconfig/lemming.pb.txt"
                sh "fkne teardown ./deploy/kne/kind-bridge.yaml"
            }
        }
    }
    post {
        cleanup {
            echo "========cleanup========"
            cleanWs()
        }
    }
}